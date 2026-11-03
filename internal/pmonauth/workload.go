package pmonauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

var (
	ErrWorkloadExchange       = errors.New("pmon refused the workload identity token")
	ErrWorkloadAccessExpired  = errors.New("the pmon access token minted from the workload identity token has expired")
	ErrWorkloadAccessRejected = errors.New("pmon rejected the access token minted from the workload identity token")
)

const (
	exchangePath      = "/api/workload/exchange"
	revokePath        = "/oauth/revoke"
	revokeTimeout     = 1500 * time.Millisecond
	expiryMargin      = 10 * time.Second
	replayWait        = 5 * time.Second
	replayPoll        = 250 * time.Millisecond
	exchangeBodyLimit = 64 * 1024
	lockWait          = 5 * time.Second
	lockStale         = 10 * time.Second
	lockPoll          = 20 * time.Millisecond
)

type WorkloadOptions struct {
	Endpoint   string
	Token      string
	Origin     string
	Scopes     []string
	CacheDir   string
	HTTPClient *http.Client
}

type WorkloadHandler struct {
	opts        WorkloadOptions
	exchangeURL string
	revokeURL   string
	holder      cacheHolder

	mu    sync.Mutex
	token *oauth2.Token
	scope string
}

var _ auth.OAuthHandler = (*WorkloadHandler)(nil)

func NewWorkloadHandler(opts WorkloadOptions) (*WorkloadHandler, error) {
	if strings.TrimSpace(opts.Token) == "" {
		return nil, errors.New("pmonauth: workload identity token is required")
	}
	exchangeURL, err := ExchangeURL(opts.Endpoint)
	if err != nil {
		return nil, err
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	h := &WorkloadHandler{opts: opts, exchangeURL: exchangeURL, revokeURL: strings.TrimSuffix(exchangeURL, exchangePath) + revokePath,
		holder: cacheHolder{PID: os.Getpid(), ID: rand.Text()}}
	openMu.Lock()
	open = append(open, h)
	openMu.Unlock()
	return h, nil
}

var (
	openMu sync.Mutex
	open   []*WorkloadHandler
)

func CloseWorkloadHandlers(ctx context.Context) error {
	openMu.Lock()
	handlers := open
	open = nil
	openMu.Unlock()
	var errs []error
	for _, h := range handlers {
		if err := h.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h *WorkloadHandler) Close(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.token == nil {
		return nil
	}
	access := h.token.AccessToken
	h.token = nil
	if !h.releaseCache(access) {
		return nil
	}
	return h.revoke(ctx, access)
}

func (h *WorkloadHandler) revoke(ctx context.Context, access string) error {
	ctx, cancel := context.WithTimeout(ctx, revokeTimeout)
	defer cancel()
	form := url.Values{"token": {access}, "token_type_hint": {"access_token"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.revokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := h.opts.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("revoking the workload access token at %s: %w", h.revokeURL, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("revoking the workload access token at %s: HTTP %d", h.revokeURL, resp.StatusCode)
	}
	return nil
}

func (h *WorkloadHandler) releaseCache(access string) bool {
	p := h.cachePath()
	if p == "" {
		return true
	}
	last := true
	h.withCacheLock(func() {
		var cached exchangedToken
		raw, err := os.ReadFile(p)
		if err != nil || json.Unmarshal(raw, &cached) != nil || cached.AccessToken != access {
			return
		}
		cached.Holders = h.otherLiveHolders(cached.Holders)
		if len(cached.Holders) > 0 {
			last = false
			writeCacheFile(p, cached)
			return
		}
		_ = os.Remove(p)
	})
	return last
}

func (h *WorkloadHandler) otherLiveHolders(holders []cacheHolder) []cacheHolder {
	var out []cacheHolder
	for _, c := range holders {
		if c != h.holder && processAlive(c.PID) {
			out = append(out, c)
		}
	}
	return out
}

func (h *WorkloadHandler) withCacheLock(fn func()) {
	lock := h.cachePath() + ".lock"
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			defer func() { _ = os.Remove(lock) }()
			break
		}
		if info, serr := os.Stat(lock); serr == nil && time.Since(info.ModTime()) > lockStale {
			_ = os.Remove(lock)
			continue
		}
		if !errors.Is(err, os.ErrExist) || time.Now().After(deadline) {
			break
		}
		time.Sleep(lockPoll)
	}
	fn()
}

func ExchangeURL(endpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("pmonauth: endpoint %q is not an absolute http(s) URL", endpoint)
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: exchangePath}).String(), nil
}

func (h *WorkloadHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.token == nil {
		if err := h.exchange(ctx); err != nil {
			return nil, err
		}
	}
	return expiringSource{token: h.token}, nil
}

func (h *WorkloadHandler) Scope() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.scope
}

func (h *WorkloadHandler) Authorize(_ context.Context, _ *http.Request, resp *http.Response) error {
	if resp == nil {
		return ErrWorkloadAccessRejected
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	if asked := challengeScopes(resp.Header.Values("WWW-Authenticate")); len(asked) > 0 && resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: the call needs %q, and the workload binding granted %q; add the scope to the binding's mcp_scopes",
			ErrWorkloadAccessRejected, strings.Join(asked, " "), h.Scope())
	}
	return ErrWorkloadAccessRejected
}

type expiringSource struct{ token *oauth2.Token }

func (s expiringSource) Token() (*oauth2.Token, error) {
	if !s.token.Expiry.IsZero() && time.Now().Add(expiryMargin).After(s.token.Expiry) {
		return nil, ErrWorkloadAccessExpired
	}
	return s.token, nil
}

type exchangedToken struct {
	AccessToken string        `json:"accessToken"`
	ExpiresAt   string        `json:"expiresAt"`
	Scope       string        `json:"scope"`
	Holders     []cacheHolder `json:"holders,omitempty"`
}

type cacheHolder struct {
	PID int    `json:"pid"`
	ID  string `json:"id"`
}

func (h *WorkloadHandler) exchange(ctx context.Context) error {
	if cached, ok := h.claimCache(); ok {
		return h.adopt(cached)
	}
	body, _ := json.Marshal(map[string]any{"token": strings.TrimSpace(h.opts.Token), "purpose": "mcp", "scopes": nonNilScopes(h.opts.Scopes)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.exchangeURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.opts.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("exchanging the workload identity token at %s: %w", h.exchangeURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, exchangeBodyLimit))
	if err != nil {
		return fmt.Errorf("reading the workload token exchange response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		exErr := newExchangeError(resp.StatusCode, raw, h.opts.Token, h.opts.Origin)
		if exErr.Code == "workload.token_replayed" {
			if cached, ok := h.awaitCache(ctx); ok {
				return h.adopt(cached)
			}
		}
		return exErr
	}
	var out exchangedToken
	if err := json.Unmarshal(raw, &out); err != nil || out.AccessToken == "" {
		return fmt.Errorf("%w: the exchange answered 200 without an access token", ErrWorkloadExchange)
	}
	if err := h.adopt(out); err != nil {
		return err
	}
	h.writeCache(out)
	return nil
}

func nonNilScopes(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (h *WorkloadHandler) adopt(t exchangedToken) error {
	expiry, err := time.Parse(time.RFC3339Nano, t.ExpiresAt)
	if err != nil {
		return fmt.Errorf("%w: the exchange answered an unreadable expiresAt %q", ErrWorkloadExchange, t.ExpiresAt)
	}
	h.token = &oauth2.Token{AccessToken: t.AccessToken, TokenType: "Bearer", Expiry: expiry}
	h.scope = t.Scope
	return nil
}

func (h *WorkloadHandler) cachePath() string {
	if h.opts.CacheDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(h.exchangeURL + "\x00" + strings.TrimSpace(h.opts.Token) + "\x00" + strings.Join(h.opts.Scopes, " ")))
	return filepath.Join(h.opts.CacheDir, "pmon-workload-"+hex.EncodeToString(sum[:12])+".json")
}

func (h *WorkloadHandler) readCache() (exchangedToken, bool) {
	var t exchangedToken
	p := h.cachePath()
	if p == "" {
		return t, false
	}
	raw, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(raw, &t) != nil || t.AccessToken == "" {
		return t, false
	}
	expiry, err := time.Parse(time.RFC3339Nano, t.ExpiresAt)
	if err != nil || time.Now().Add(expiryMargin).After(expiry) {
		_ = os.Remove(p)
		return t, false
	}
	return t, true
}

func (h *WorkloadHandler) awaitCache(ctx context.Context) (exchangedToken, bool) {
	if h.cachePath() == "" {
		return exchangedToken{}, false
	}
	deadline := time.Now().Add(replayWait)
	for time.Now().Before(deadline) {
		if t, ok := h.claimCache(); ok {
			return t, true
		}
		select {
		case <-ctx.Done():
			return exchangedToken{}, false
		case <-time.After(replayPoll):
		}
	}
	return exchangedToken{}, false
}

func (h *WorkloadHandler) claimCache() (exchangedToken, bool) {
	if h.cachePath() == "" {
		return exchangedToken{}, false
	}
	var t exchangedToken
	var ok bool
	h.withCacheLock(func() {
		if t, ok = h.readCache(); ok {
			t.Holders = append(h.otherLiveHolders(t.Holders), h.holder)
			writeCacheFile(h.cachePath(), t)
		}
	})
	return t, ok
}

func (h *WorkloadHandler) writeCache(t exchangedToken) {
	p := h.cachePath()
	if p == "" {
		return
	}
	h.withCacheLock(func() {
		t.Holders = []cacheHolder{h.holder}
		writeCacheFile(p, t)
	})
}

func writeCacheFile(p string, t exchangedToken) {
	raw, _ := json.Marshal(t)
	tmp, err := os.CreateTemp(filepath.Dir(p), ".pmon-workload-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Chmod(tmp.Name(), 0o600) != nil || os.Rename(tmp.Name(), p) != nil {
		_ = os.Remove(tmp.Name())
	}
}

type ExchangeError struct {
	Status  int
	Code    string
	Subject string
	Origin  string
}

func (e *ExchangeError) Unwrap() error { return ErrWorkloadExchange }

func (e *ExchangeError) Error() string {
	from := ""
	if e.Origin != "" {
		from = " (from " + e.Origin + ")"
	}
	subject := ""
	if e.Subject != "" {
		subject = " with subject " + e.Subject
	}
	var why string
	switch e.Code {
	case "workload.not_configured":
		why = "this pmon trusts no workload issuers; its operator sets PM_CI_OIDC_ISSUERS"
	case "workload.issuer_not_trusted":
		why = "the token's issuer is not one this pmon trusts (PM_CI_OIDC_ISSUERS)"
	case "common.invalid_token":
		why = "pmon could not verify the token: check that its audience is pmon's PM_CI_OIDC_AUDIENCE (by default pmon's own origin) and that it has not expired"
	case "workload.token_replayed":
		why = "this token was already exchanged, and each token exchanges once; a run that configures the provider more than once needs a fresh token per configuration"
	case "workload.no_binding":
		why = "no enabled workload binding matches the token" + subject + "; create one with pmon_workload_binding or in pmon's console"
	case "workload.ambiguous_binding":
		why = "more than one workload binding matches the token" + subject + "; narrow their subject or conditions so exactly one matches"
	case "workload.purpose_not_allowed":
		why = "the matching workload binding does not allow the mcp purpose; add mcp to its purposes"
	case "workload.scope_not_allowed":
		why = "the provider asked for a scope the matching workload binding does not grant; widen the binding's mcp_scopes or narrow the provider's scopes"
	case "":
		why = fmt.Sprintf("the exchange answered HTTP %d", e.Status)
	default:
		why = fmt.Sprintf("the exchange answered HTTP %d (%s)", e.Status, e.Code)
	}
	return ErrWorkloadExchange.Error() + from + ": " + why
}

func newExchangeError(status int, body []byte, jwt, origin string) *ExchangeError {
	var apiErr struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &apiErr)
	return &ExchangeError{Status: status, Code: apiErr.Code, Subject: unverifiedSubject(jwt), Origin: origin}
}

func unverifiedSubject(jwt string) string {
	parts := strings.Split(strings.TrimSpace(jwt), ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return ""
	}
	return claims.Sub
}

package pmonauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeJWT(sub string) string {
	part := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return part(map[string]string{"alg": "RS256"}) + "." + part(map[string]string{"sub": sub}) + ".sig"
}

type exchangeServer struct {
	srv     *httptest.Server
	calls   atomic.Int32
	status  int
	code    string
	expiry  time.Time
	got     map[string]any
	revoked []string
}

func newExchangeServer(t *testing.T) *exchangeServer {
	t.Helper()
	es := &exchangeServer{status: http.StatusOK, expiry: time.Now().Add(10 * time.Minute)}
	es.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		es.calls.Add(1)
		if r.URL.Path == "/oauth/revoke" && r.Method == http.MethodPost {
			_ = r.ParseForm()
			es.revoked = append(es.revoked, r.PostForm.Get("token"))
			_, _ = w.Write([]byte("{}"))
			return
		}
		if r.URL.Path != "/api/workload/exchange" || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "unexpected request", http.StatusTeapot)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &es.got)
		w.Header().Set("Content-Type", "application/json")
		if es.status != http.StatusOK {
			w.WriteHeader(es.status)
			_, _ = w.Write([]byte(`{"code":"` + es.code + `","params":{}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"accessToken": "pma_minted", "expiresAt": es.expiry.UTC().Format(time.RFC3339Nano), "scope": "mcp:read"})
	}))
	t.Cleanup(es.srv.Close)
	return es
}

func (es *exchangeServer) handler(t *testing.T, opts WorkloadOptions) *WorkloadHandler {
	t.Helper()
	opts.Endpoint = es.srv.URL + "/mcp"
	if opts.Token == "" {
		opts.Token = fakeJWT("organization:acme:project:infra:workspace:pmon:run_phase:plan")
	}
	h, err := NewWorkloadHandler(opts)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestExchangeURLIsTheEndpointOrigin(t *testing.T) {
	for endpoint, want := range map[string]string{
		"https://pmon.example.com/mcp":      "https://pmon.example.com/api/workload/exchange",
		"https://pmon.example.com:8443/mcp": "https://pmon.example.com:8443/api/workload/exchange",
		"http://127.0.0.1:8090/mcp?x=1":     "http://127.0.0.1:8090/api/workload/exchange",
	} {
		if got, err := ExchangeURL(endpoint); err != nil || got != want {
			t.Errorf("%s: %q %v", endpoint, got, err)
		}
	}
	for _, bad := range []string{"", "pmon.example.com/mcp", "ftp://pmon.example.com/mcp"} {
		if _, err := ExchangeURL(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestWorkloadHandlerExchangesOnceForTheMCPPurpose(t *testing.T) {
	es := newExchangeServer(t)
	h := es.handler(t, WorkloadOptions{Scopes: []string{"mcp:read"}})
	for range 3 {
		source, err := h.TokenSource(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		token, err := source.Token()
		if err != nil || token.AccessToken != "pma_minted" || token.Type() != "Bearer" {
			t.Fatalf("token %+v %v", token, err)
		}
	}
	if n := es.calls.Load(); n != 1 {
		t.Errorf("%d exchanges", n)
	}
	if scopes, ok := es.got["scopes"].([]any); es.got["purpose"] != "mcp" || es.got["token"] == "" || !ok || len(scopes) != 1 {
		t.Errorf("request %v", es.got)
	}
	if h.Scope() != "mcp:read" {
		t.Errorf("scope %q", h.Scope())
	}
}

func TestWorkloadHandlerRequestsNoScopesByDefault(t *testing.T) {
	es := newExchangeServer(t)
	if _, err := es.handler(t, WorkloadOptions{}).TokenSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scopes, ok := es.got["scopes"].([]any); !ok || len(scopes) != 0 {
		t.Errorf("scopes %v", es.got["scopes"])
	}
}

func TestWorkloadHandlerReportsExpiryInsteadOfRenewing(t *testing.T) {
	es := newExchangeServer(t)
	es.expiry = time.Now().Add(5 * time.Second)
	source, err := es.handler(t, WorkloadOptions{}).TokenSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Token(); !errors.Is(err, ErrWorkloadAccessExpired) || !strings.Contains(err.Error(), "PM_OAUTH_ACCESS_TTL") {
		t.Errorf("got %v", err)
	}
}

func TestWorkloadHandlerExplainsEachRefusal(t *testing.T) {
	for code, want := range map[string]string{
		"workload.no_binding":          "subject organization:acme:project:infra:workspace:pmon:run_phase:plan",
		"workload.ambiguous_binding":   "more than one workload binding",
		"workload.token_replayed":      "exchanges once",
		"workload.scope_not_allowed":   "mcp_scopes",
		"workload.purpose_not_allowed": "mcp purpose",
		"workload.issuer_not_trusted":  "PM_CI_OIDC_ISSUERS",
		"common.invalid_token":         "PM_CI_OIDC_AUDIENCE",
	} {
		es := newExchangeServer(t)
		es.status, es.code = http.StatusForbidden, code
		_, err := es.handler(t, WorkloadOptions{Origin: "TFC_WORKLOAD_IDENTITY_TOKEN_PMON"}).TokenSource(context.Background())
		var exErr *ExchangeError
		if !errors.As(err, &exErr) || !errors.Is(err, ErrWorkloadExchange) || exErr.Code != code ||
			!strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "TFC_WORKLOAD_IDENTITY_TOKEN_PMON") {
			t.Errorf("%s: %v", code, err)
		}
	}
}

func TestWorkloadHandlerSharesOneExchangeThroughTheRunCache(t *testing.T) {
	es := newExchangeServer(t)
	dir := t.TempDir()
	if _, err := es.handler(t, WorkloadOptions{CacheDir: dir}).TokenSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	es.status, es.code = http.StatusUnauthorized, "workload.token_replayed"
	source, err := es.handler(t, WorkloadOptions{CacheDir: dir}).TokenSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token, err := source.Token(); err != nil || token.AccessToken != "pma_minted" {
		t.Errorf("token %+v %v", token, err)
	}
	if n := es.calls.Load(); n != 1 {
		t.Errorf("%d exchanges, want the second process to reuse the cached one", n)
	}
}

func TestWorkloadHandlerAuthorizeNamesAMissingScope(t *testing.T) {
	es := newExchangeServer(t)
	h := es.handler(t, WorkloadOptions{})
	if _, err := h.TokenSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := &closeRecorder{Reader: strings.NewReader("")}
	resp := &http.Response{StatusCode: http.StatusForbidden, Body: body, Header: http.Header{}}
	resp.Header.Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="mcp:policies:write"`)
	err := h.Authorize(context.Background(), nil, resp)
	if !errors.Is(err, ErrWorkloadAccessRejected) || !strings.Contains(err.Error(), "mcp:policies:write") || !body.closed {
		t.Errorf("got %v, closed %v", err, body.closed)
	}
	if err := h.Authorize(context.Background(), nil, &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(""))}); !errors.Is(err, ErrWorkloadAccessRejected) {
		t.Errorf("401: %v", err)
	}
}

func forgetOpenHandlers() {
	openMu.Lock()
	open = nil
	openMu.Unlock()
}

func TestClosingTheProviderRevokesTheTokenAndDeletesItsCache(t *testing.T) {
	forgetOpenHandlers()
	es := newExchangeServer(t)
	dir := t.TempDir()
	h := es.handler(t, WorkloadOptions{CacheDir: dir})
	if _, err := h.TokenSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	cached, _ := filepath.Glob(filepath.Join(dir, "pmon-workload-*.json"))
	if len(cached) != 1 {
		t.Fatalf("cache files %v", cached)
	}
	if info, err := os.Stat(cached[0]); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode %v %v", info, err)
	}
	if err := CloseWorkloadHandlers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(es.revoked) != 1 || es.revoked[0] != "pma_minted" {
		t.Errorf("revoked %v", es.revoked)
	}
	if _, err := os.Stat(cached[0]); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the cache outlived the provider: %v", err)
	}
	if err := h.Close(context.Background()); err != nil || len(es.revoked) != 1 {
		t.Errorf("a second close revoked again: %v %v", err, es.revoked)
	}
}

func TestAnExpiredRunCacheIsDeletedWhenRead(t *testing.T) {
	es := newExchangeServer(t)
	dir := t.TempDir()
	es.expiry = time.Now().Add(5 * time.Second)
	if _, err := es.handler(t, WorkloadOptions{CacheDir: dir}).TokenSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	es.expiry = time.Now().Add(10 * time.Minute)
	if _, err := es.handler(t, WorkloadOptions{CacheDir: dir}).TokenSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := es.calls.Load(); n != 2 {
		t.Errorf("%d exchanges, want the stale cache ignored", n)
	}
	forgetOpenHandlers()
}

func TestOnlyTheLastHolderOfTheRunCacheRevokes(t *testing.T) {
	forgetOpenHandlers()
	es := newExchangeServer(t)
	dir := t.TempDir()
	first := es.handler(t, WorkloadOptions{CacheDir: dir})
	second := es.handler(t, WorkloadOptions{CacheDir: dir})
	for _, h := range []*WorkloadHandler{first, second} {
		if _, err := h.TokenSource(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := es.calls.Load(); n != 1 {
		t.Fatalf("%d exchanges, want the second handler to adopt the cache", n)
	}
	if err := first.Close(context.Background()); err != nil || len(es.revoked) != 0 {
		t.Fatalf("the first close revoked a token the second still holds: %v %v", err, es.revoked)
	}
	cached, _ := filepath.Glob(filepath.Join(dir, "pmon-workload-*.json"))
	if len(cached) != 1 {
		t.Fatalf("the cache went away while the second handler holds it: %v", cached)
	}
	if err := second.Close(context.Background()); err != nil || len(es.revoked) != 1 || es.revoked[0] != "pma_minted" {
		t.Fatalf("the last close did not revoke: %v %v", err, es.revoked)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "pmon-workload-*")); len(left) != 0 {
		t.Errorf("files left behind: %v", left)
	}
	forgetOpenHandlers()
}

func TestADeadHolderDoesNotKeepTheTokenAlive(t *testing.T) {
	forgetOpenHandlers()
	es := newExchangeServer(t)
	dir := t.TempDir()
	h := es.handler(t, WorkloadOptions{CacheDir: dir})
	if _, err := h.TokenSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(h.cachePath())
	if err != nil {
		t.Fatal(err)
	}
	var cached exchangedToken
	if err := json.Unmarshal(raw, &cached); err != nil || len(cached.Holders) != 1 || cached.Holders[0] != h.holder {
		t.Fatalf("holders %+v %v", cached.Holders, err)
	}
	cached.Holders = append(cached.Holders, cacheHolder{PID: 1 << 30, ID: "gone"})
	writeCacheFile(h.cachePath(), cached)
	if err := h.Close(context.Background()); err != nil || len(es.revoked) != 1 {
		t.Errorf("a dead holder kept the token: %v %v", err, es.revoked)
	}
	forgetOpenHandlers()
}

func TestTheCacheLockSerializesAndRecoversWhenStale(t *testing.T) {
	es := newExchangeServer(t)
	h := es.handler(t, WorkloadOptions{CacheDir: t.TempDir()})
	lock := h.cachePath() + ".lock"
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockStale)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	ran := false
	locked := h.withCacheLock(lockWait, func() {
		if _, err := os.Stat(lock); err != nil {
			t.Errorf("the lock is not held while the work runs: %v", err)
		}
		ran = true
	})
	if _, err := os.Stat(lock); !locked || !ran || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("locked %v, ran %v, lock after %v", locked, ran, err)
	}
	forgetOpenHandlers()
}

func TestCloseKeepsTheTokenWhenTheCacheLockIsBusy(t *testing.T) {
	forgetOpenHandlers()
	es := newExchangeServer(t)
	h := es.handler(t, WorkloadOptions{CacheDir: t.TempDir()})
	if _, err := h.TokenSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	lock := h.cachePath() + ".lock"
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if h.withCacheLock(0, func() {}) {
		t.Fatal("took a lock another holder has")
	}
	start := time.Now()
	if err := h.Close(context.Background()); err != nil || len(es.revoked) != 0 {
		t.Errorf("revoked without the cache lock: %v %v", err, es.revoked)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("close waited %v for the lock", took)
	}
	if _, err := os.Stat(h.cachePath()); err != nil {
		t.Errorf("the cache went away without the lock: %v", err)
	}
	forgetOpenHandlers()
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) || processAlive(0) || processAlive(1<<30) {
		t.Error("processAlive misreads this process, pid 0, or an unused pid")
	}
}

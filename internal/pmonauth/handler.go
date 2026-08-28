// Package pmonauth logs the provider in to a pmon MCP endpoint.
//
// pmon is an OAuth 2.1 resource server with a co-hosted authorization server, and it has no
// dynamic client registration: a client identifies itself by the URL of its Client ID Metadata
// Document. The SDK's authorization-code handler covers the protocol; what this package adds is
// where the token is kept, when a browser is opened, and the refusal to open one at all when the
// caller says not to.
package pmonauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

// ErrBrowserDisabled is returned when a login is needed but the caller forbade opening a browser.
// Terraform surfaces it as a diagnostic rather than hanging on a login nobody can complete.
var ErrBrowserDisabled = errors.New("pmon login requires a browser, but browser login is disabled")

// Options configures a Handler.
type Options struct {
	// Endpoint is the pmon MCP endpoint. It also keys the token cache.
	Endpoint string
	// ClientMetadataURL is the HTTPS URL of this client's metadata document, which serves as the
	// OAuth client id.
	ClientMetadataURL string
	// CachePath is where tokens are stored between runs.
	CachePath string
	// NoBrowser makes a required login fail with ErrBrowserDisabled instead of opening a browser.
	NoBrowser bool
	// Scopes caps what a login may ask for. Empty means whatever pmon asks for.
	Scopes []string
	// Progress receives human-readable login progress. Nil discards it.
	Progress io.Writer
}

// Handler satisfies the SDK's auth.OAuthHandler. It answers from cache where it can and runs the
// full authorization-code flow only when it must.
type Handler struct {
	opts  Options
	cache *cache

	// login serializes the authorization-code flow. Terraform calls tools concurrently, so a
	// token that expires mid-run draws one 401 per in-flight request, and each one would
	// otherwise open its own browser tab and its own loopback listener.
	login sync.Mutex

	mu     sync.Mutex
	source oauth2.TokenSource
}

var _ auth.OAuthHandler = (*Handler)(nil)

// NewHandler validates opts and returns a Handler. It performs no I/O: nothing is read or
// fetched until the transport asks for a token.
func NewHandler(opts Options) (*Handler, error) {
	if opts.Endpoint == "" {
		return nil, errors.New("pmonauth: endpoint is required")
	}
	if opts.ClientMetadataURL == "" {
		return nil, errors.New("pmonauth: client metadata URL is required")
	}
	if opts.CachePath == "" {
		return nil, errors.New("pmonauth: cache path is required")
	}
	return &Handler{opts: opts, cache: newCache(opts.CachePath)}, nil
}

// TokenSource returns a source backed by the cached credential, refreshing it if it has expired.
// A nil source is not an error: it tells the transport to send the request unauthenticated, take
// the 401, and call Authorize.
func (h *Handler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.source != nil {
		return h.source, nil
	}

	cached := h.cache.load(h.opts.Endpoint)
	if cached == nil {
		return nil, nil
	}

	// A cached token predates the current ceiling. Spending it would let a narrowed scopes
	// argument keep using the wider grant it was meant to give up.
	if !scopesWithin(cached.Scopes, h.opts.Scopes) {
		h.cache.drop(h.opts.Endpoint)
		return nil, nil
	}

	cfg := &oauth2.Config{
		ClientID: cached.ClientID,
		Scopes:   cached.Scopes,
		Endpoint: oauth2.Endpoint{TokenURL: cached.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}

	// Refresh eagerly. The transport only calls Authorize on a 401 response, so a token source
	// that fails at request time would surface as a transport error and never reach the login
	// flow. Better to find out here, drop the dead entry, and let Authorize run.
	source := cfg.TokenSource(ctx, cached.Token)
	token, err := source.Token()
	if err != nil {
		h.cache.drop(h.opts.Endpoint)
		return nil, nil
	}

	if token.AccessToken != cached.Token.AccessToken {
		h.store(cfg, token)
	}

	h.source = &persistingSource{handler: h, config: cfg, base: source, last: token}
	return h.source, nil
}

// currentSource reports the source the handler holds, which is how a caller waiting on the login
// lock tells a login that finished ahead of it from one that never ran.
func (h *Handler) currentSource() oauth2.TokenSource {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.source
}

func (h *Handler) grantCovers(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	asked := challengeScopes(resp.Header.Values("WWW-Authenticate"))
	cached := h.cache.load(h.opts.Endpoint)
	if len(asked) == 0 || cached == nil || len(cached.Scopes) == 0 {
		return false
	}
	return scopesWithin(asked, cached.Scopes)
}

// beginLogin takes the login lock and reports whether the caller still has to log in. A source
// that changed while the caller waited is a login another goroutine already completed, and the
// transport retries the request once after Authorize returns, so a caller whose scopes that login
// covered gets its token without a second trip to the browser.
func (h *Handler) beginLogin(before oauth2.TokenSource, resp *http.Response) (release func(), needed bool) {
	h.login.Lock()
	if h.currentSource() != before && h.grantCovers(resp) {
		h.login.Unlock()
		return func() {}, false
	}
	return h.login.Unlock, true
}

// Authorize runs the authorization-code flow: discovery, CIMD registration, PKCE, and a browser
// round trip. The transport calls it after a 401 and retries the request once it returns nil.
func (h *Handler) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	if h.opts.NoBrowser {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return ErrBrowserDisabled
	}

	release, needed := h.beginLogin(h.currentSource(), resp)
	defer release()
	if !needed {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil
	}

	if err := h.applyScopeCeiling(resp); err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return err
	}

	listener, redirectURL, err := listenLoopback()
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return err
	}
	defer func() { _ = listener.Close() }()

	inner, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		ClientIDMetadataDocumentConfig: &auth.ClientIDMetadataDocumentConfig{URL: h.opts.ClientMetadataURL},
		RedirectURL:                    redirectURL,
		AuthorizationCodeFetcher:       browserFetcher(listener, h.notify),
		RequestRefreshToken:            true,
		NewTokenSource:                 h.newTokenSource,
	})
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return fmt.Errorf("preparing the pmon login: %w", err)
	}

	if err := inner.Authorize(ctx, req, resp); err != nil {
		return fmt.Errorf("logging in to pmon at %s: %w", h.opts.Endpoint, err)
	}

	source, err := inner.TokenSource(ctx)
	if err != nil {
		return fmt.Errorf("reading the pmon token after login: %w", err)
	}

	h.mu.Lock()
	h.source = source
	h.mu.Unlock()
	return nil
}

// applyScopeCeiling rewrites the challenge in place so the inner handler reads a narrowed scope
// set. The transport hands the response over before the handler sees it, and that is the only
// moment at which the request it is about to build can still be capped.
func (h *Handler) applyScopeCeiling(resp *http.Response) error {
	if resp == nil || len(h.opts.Scopes) == 0 {
		return nil
	}

	narrowed, err := narrowChallenge(resp.Header.Values("WWW-Authenticate"), h.opts.Scopes)
	if err != nil {
		return err
	}

	resp.Header.Del("WWW-Authenticate")
	for _, value := range narrowed {
		resp.Header.Add("WWW-Authenticate", value)
	}
	return nil
}

// newTokenSource is the SDK's post-exchange hook. It hands over the resolved config, which is the
// only place the discovered token endpoint is visible -- and the reason a later run can refresh
// without repeating discovery.
func (h *Handler) newTokenSource(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
	h.store(cfg, token)
	return &persistingSource{handler: h, config: cfg, base: cfg.TokenSource(ctx, token), last: token}, nil
}

// storeFrom writes a rotation back only when src is still the handler's source. A step-up login
// replaces that source with one holding a wider grant, and the transport can still be holding the
// one it replaced. Letting the old one write would put its narrower scope list back beside the
// newer token, and scopesWithin would then read an understatement of what the token can do.
func (h *Handler) storeFrom(src oauth2.TokenSource, cfg *oauth2.Config, token *oauth2.Token) {
	h.mu.Lock()
	superseded := h.source != nil && h.source != src
	h.mu.Unlock()
	if superseded {
		return
	}
	h.store(cfg, token)
}

func (h *Handler) store(cfg *oauth2.Config, token *oauth2.Token) {
	err := h.cache.save(h.opts.Endpoint, &entry{
		TokenURL: cfg.Endpoint.TokenURL,
		ClientID: cfg.ClientID,
		Scopes:   grantedScopes(cfg, token),
		Token:    token,
	})
	if err != nil {
		h.notify(fmt.Sprintf("Could not cache the pmon token, the next run will log in again: %v", err))
	}
}

// grantedScopes reports what the token can actually do, preferring the authorization server's
// answer to the client's request. RFC 6749 section 5.1 has the token response carry its own
// `scope` whenever the grant differs from what was asked for, and pmon sends it on every
// response. The request is only a fallback, for a server that stays silent.
func grantedScopes(cfg *oauth2.Config, token *oauth2.Token) []string {
	if token != nil {
		if scope, ok := token.Extra("scope").(string); ok {
			if granted := strings.Fields(scope); len(granted) > 0 {
				return granted
			}
		}
	}
	return cfg.Scopes
}

func (h *Handler) notify(message string) {
	if h.opts.Progress == nil {
		return
	}
	_, _ = fmt.Fprintln(h.opts.Progress, message)
}

// persistingSource writes each rotated token back to the cache. pmon rotates its refresh token on
// every use, so a token that is not written back is a login the next run has to repeat.
type persistingSource struct {
	handler *Handler
	config  *oauth2.Config

	mu   sync.Mutex
	base oauth2.TokenSource
	last *oauth2.Token
}

func (s *persistingSource) Token() (*oauth2.Token, error) {
	token, err := s.base.Token()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	rotated := s.last == nil || token.AccessToken != s.last.AccessToken
	s.last = token
	s.mu.Unlock()

	if rotated {
		s.handler.storeFrom(s, s.config, token)
	}
	return token, nil
}

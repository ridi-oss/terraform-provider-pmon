package pmonauth

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func testOptions(t *testing.T) Options {
	t.Helper()
	return Options{
		Endpoint:          "https://pmon.example.com/mcp",
		ClientMetadataURL: "https://example.github.io/client-metadata.json",
		CachePath:         filepath.Join(t.TempDir(), "token.json"),
	}
}

func TestNewHandlerRequiresEndpointAndClientID(t *testing.T) {
	for name, mutate := range map[string]func(*Options){
		"no endpoint":   func(o *Options) { o.Endpoint = "" },
		"no client id":  func(o *Options) { o.ClientMetadataURL = "" },
		"no cache path": func(o *Options) { o.CachePath = "" },
	} {
		t.Run(name, func(t *testing.T) {
			opts := testOptions(t)
			mutate(&opts)
			if _, err := NewHandler(opts); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// No cache means no token source, which is how the transport learns to take a 401 and log in.
func TestTokenSourceIsNilWithoutCache(t *testing.T) {
	h, err := NewHandler(testOptions(t))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	source, err := h.TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	if source != nil {
		t.Errorf("TokenSource = %v, want nil so the transport calls Authorize", source)
	}
}

// A cached token whose refresh no longer works must not wedge the provider: the entry is dropped
// and the next step is an ordinary login.
func TestTokenSourceDropsUnrefreshableEntry(t *testing.T) {
	opts := testOptions(t)
	h, err := NewHandler(opts)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	expired := testEntry()
	expired.Token.Expiry = expired.Token.Expiry.Add(-2 * 60 * 60 * 1e9)
	expired.TokenURL = "https://127.0.0.1:1/oauth/token"
	if err := h.cache.save(opts.Endpoint, expired); err != nil {
		t.Fatalf("seeding the cache: %v", err)
	}

	source, err := h.TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	if source != nil {
		t.Errorf("TokenSource = %v, want nil after an unusable entry is dropped", source)
	}
	if got := h.cache.load(opts.Endpoint); got != nil {
		t.Errorf("cache still holds %+v, want the dead entry dropped", got)
	}
}

func TestAuthorizeRefusesWithoutBrowser(t *testing.T) {
	opts := testOptions(t)
	opts.NoBrowser = true
	h, err := NewHandler(opts)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, opts.Endpoint, nil)
	if err != nil {
		t.Fatalf("building a request: %v", err)
	}

	err = h.Authorize(context.Background(), req, nil)
	if !errors.Is(err, ErrBrowserDisabled) {
		t.Errorf("Authorize error = %v, want ErrBrowserDisabled", err)
	}
}

// The redirect the provider listens on has to line up with the portless loopback URI declared in
// the client metadata document: same scheme, same host, same path, differing only by port.
func TestListenLoopbackMatchesDeclaredRedirect(t *testing.T) {
	ln, redirect, err := listenLoopback()
	if err != nil {
		t.Fatalf("listenLoopback: %v", err)
	}
	defer func() { _ = ln.Close() }()

	if !strings.HasPrefix(redirect, "http://127.0.0.1:") {
		t.Errorf("redirect = %q, want a loopback URL with an ephemeral port", redirect)
	}
	if !strings.HasSuffix(redirect, callbackPath) {
		t.Errorf("redirect = %q, want it to end in %q", redirect, callbackPath)
	}
}

// A token cached before the ceiling was tightened still carries the wider grant. Reusing it would
// quietly defeat a narrowed scopes argument, so it is dropped and the next step is a fresh login.
func TestTokenSourceDropsAnOverScopedEntry(t *testing.T) {
	opts := testOptions(t)
	opts.Scopes = []string{"mcp:read"}
	h, err := NewHandler(opts)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	wide := testEntry()
	wide.Scopes = []string{"mcp:read", "mcp:identity:write"}
	if err := h.cache.save(opts.Endpoint, wide); err != nil {
		t.Fatalf("seeding the cache: %v", err)
	}

	source, err := h.TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	if source != nil {
		t.Errorf("TokenSource = %v, want nil so a narrower login runs", source)
	}
	if got := h.cache.load(opts.Endpoint); got != nil {
		t.Errorf("cache still holds %+v, want the over-scoped entry dropped", got)
	}
}

// pmon answers the token request with the scope it actually granted, which is not always the
// scope that was asked for -- a step-up asks for one scope and comes back with the union. The
// cache has to hold what the token can do, because that is what the ceiling check reads.
func TestStoreRecordsTheGrantedScope(t *testing.T) {
	opts := testOptions(t)
	h, err := NewHandler(opts)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	cfg := &oauth2.Config{
		ClientID: "https://example.github.io/client-metadata.json",
		Scopes:   []string{"mcp:identity:write"}, // what the step-up asked for
		Endpoint: oauth2.Endpoint{TokenURL: "https://pmon.example.com/oauth/token"},
	}
	token := (&oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}).
		WithExtra(map[string]any{"scope": "mcp:read mcp:identity:write"}) // what pmon granted

	h.store(cfg, token)

	cached := h.cache.load(opts.Endpoint)
	if cached == nil {
		t.Fatal("nothing cached")
	}
	if got := strings.Join(cached.Scopes, " "); got != "mcp:read mcp:identity:write" {
		t.Errorf("cached scopes = %q, want the granted set, not the requested one", got)
	}
}

// Without a scope in the response there is nothing better to record than the request.
func TestStoreFallsBackToTheRequestedScope(t *testing.T) {
	opts := testOptions(t)
	h, err := NewHandler(opts)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	cfg := &oauth2.Config{
		ClientID: "https://example.github.io/client-metadata.json",
		Scopes:   []string{"mcp:read"},
		Endpoint: oauth2.Endpoint{TokenURL: "https://pmon.example.com/oauth/token"},
	}
	h.store(cfg, &oauth2.Token{AccessToken: "a", Expiry: time.Now().Add(time.Hour)})

	cached := h.cache.load(opts.Endpoint)
	if cached == nil {
		t.Fatal("nothing cached")
	}
	if got := strings.Join(cached.Scopes, " "); got != "mcp:read" {
		t.Errorf("cached scopes = %q, want the requested set", got)
	}
}

// The regression this was written for: a step-up widens the grant, and the source it replaced is
// still live in the transport. When that one rotates it must not put its narrower scopes back.
func TestSupersededSourceDoesNotOverwriteTheCache(t *testing.T) {
	opts := testOptions(t)
	h, err := NewHandler(opts)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	narrow := &oauth2.Config{
		ClientID: "https://example.github.io/client-metadata.json",
		Scopes:   []string{"mcp:read"},
		Endpoint: oauth2.Endpoint{TokenURL: "https://pmon.example.com/oauth/token"},
	}
	stale := &persistingSource{handler: h, config: narrow}

	// The step-up result: a wider grant, and the handler now points at the source holding it.
	wide := &oauth2.Config{
		ClientID: narrow.ClientID,
		Scopes:   []string{"mcp:read", "mcp:identity:write"},
		Endpoint: narrow.Endpoint,
	}
	current := &persistingSource{handler: h, config: wide}
	h.source = current
	h.store(wide, &oauth2.Token{AccessToken: "wide", Expiry: time.Now().Add(time.Hour)})

	// The transport still holds the old source, and its token rotates.
	stale.handler.storeFrom(stale, stale.config, &oauth2.Token{AccessToken: "rotated", Expiry: time.Now().Add(time.Hour)})

	cached := h.cache.load(opts.Endpoint)
	if cached == nil {
		t.Fatal("nothing cached")
	}
	if got := strings.Join(cached.Scopes, " "); got != "mcp:read mcp:identity:write" {
		t.Errorf("cached scopes = %q, want the step-up grant left intact", got)
	}
	if cached.Token.AccessToken != "wide" {
		t.Errorf("cached token = %q, want the superseded source to have written nothing", cached.Token.AccessToken)
	}
}

func TestBeginLogin(t *testing.T) {
	for name, tc := range map[string]struct {
		granted []string
		asked   string
		raced   bool
		want    bool
	}{
		"nobody logged in while the caller waited":  {granted: []string{"mcp:read"}, asked: "mcp:read", raced: false, want: true},
		"the finished login covers the challenge":   {granted: []string{"mcp:read"}, asked: "mcp:read", raced: true, want: false},
		"the finished login is narrower than asked": {granted: []string{"mcp:read"}, asked: "mcp:identity:write", raced: true, want: true},
	} {
		t.Run(name, func(t *testing.T) {
			h, err := NewHandler(testOptions(t))
			if err != nil {
				t.Fatalf("NewHandler: %v", err)
			}

			before := h.currentSource()
			if tc.raced {
				h.mu.Lock()
				h.source = &persistingSource{handler: h}
				h.mu.Unlock()
				if err := h.cache.save(h.opts.Endpoint, &entry{
					Scopes: tc.granted,
					Token:  &oauth2.Token{AccessToken: "fresh"},
				}); err != nil {
					t.Fatalf("save: %v", err)
				}
			}

			resp := &http.Response{Header: http.Header{}}
			resp.Header.Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="`+tc.asked+`"`)

			release, needed := h.beginLogin(before, resp)
			defer release()
			if needed != tc.want {
				t.Errorf("beginLogin needed = %v, want %v", needed, tc.want)
			}
		})
	}
}

package pmonauth

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
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

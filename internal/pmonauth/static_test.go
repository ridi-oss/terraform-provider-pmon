package pmonauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestNewStaticHandlerRequiresAToken(t *testing.T) {
	for name, token := range map[string]string{"empty": "", "blank": "   "} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewStaticHandler(token); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestStaticHandlerPresentsTheToken(t *testing.T) {
	h, err := NewStaticHandler("borrowed-token")
	if err != nil {
		t.Fatalf("NewStaticHandler: %v", err)
	}

	source, err := h.TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	token, err := source.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.AccessToken != "borrowed-token" {
		t.Errorf("access token = %q", token.AccessToken)
	}
	if token.Type() != "Bearer" {
		t.Errorf("token type = %q, want Bearer", token.Type())
	}
}

// The transport retries once after Authorize returns nil. Returning nil here would spend the
// same rejected token again and report the second failure instead of the real one.
func TestStaticHandlerAuthorizeReportsRejection(t *testing.T) {
	h, err := NewStaticHandler("borrowed-token")
	if err != nil {
		t.Fatalf("NewStaticHandler: %v", err)
	}

	body := &closeRecorder{Reader: strings.NewReader("")}
	resp := &http.Response{StatusCode: http.StatusUnauthorized, Body: body}

	if err := h.Authorize(context.Background(), nil, resp); !errors.Is(err, ErrStaticTokenRejected) {
		t.Errorf("Authorize error = %v, want ErrStaticTokenRejected", err)
	}
	if !body.closed {
		t.Error("Authorize left the response body open")
	}
}

// A borrowed credential must not end up in this machine's token cache.
func TestStaticHandlerWritesNothing(t *testing.T) {
	dir := t.TempDir()

	h, err := NewStaticHandler("borrowed-token")
	if err != nil {
		t.Fatalf("NewStaticHandler: %v", err)
	}
	source, err := h.TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	if _, err := source.Token(); err != nil {
		t.Fatalf("Token: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("handler wrote %d files, want none", len(entries))
	}
}

type closeRecorder struct {
	io.Reader
	closed bool
}

func (c *closeRecorder) Close() error {
	c.closed = true
	return nil
}

package pmonauth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

// ErrStaticTokenRejected reports that pmon refused a token the caller supplied. There is no
// refresh token beside it and no login to fall back on, so the run stops here rather than
// retrying an identical request.
var ErrStaticTokenRejected = errors.New("pmon rejected the supplied access token")

// StaticHandler authenticates with a token obtained elsewhere. It skips the authorization-code
// flow entirely: nothing is discovered, no browser opens, and nothing is cached. The token
// belongs to whoever logged in to get it and this process is only borrowing it.
type StaticHandler struct {
	source oauth2.TokenSource
}

var _ auth.OAuthHandler = (*StaticHandler)(nil)

// NewStaticHandler returns a handler that presents token as a bearer credential.
func NewStaticHandler(token string) (*StaticHandler, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("pmonauth: access token is required")
	}
	return &StaticHandler{
		source: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token, TokenType: "Bearer"}),
	}, nil
}

func (h *StaticHandler) TokenSource(context.Context) (oauth2.TokenSource, error) {
	return h.source, nil
}

// Authorize is what the transport calls on a 401 or 403. A supplied token cannot be renewed from
// here, so this reports the refusal instead of letting the transport retry the same credential.
func (h *StaticHandler) Authorize(_ context.Context, _ *http.Request, resp *http.Response) error {
	// The interface makes the callee responsible for the response body.
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return ErrStaticTokenRejected
}

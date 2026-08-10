package pmonauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// callbackPath must match the path in the client metadata document's loopback redirect URIs.
// pmon matches a portless loopback redirect while ignoring the port, but the path is compared
// exactly.
const callbackPath = "/callback"

// loginTimeout bounds how long a login waits at the browser before giving up, so a terraform run
// cannot hang forever on a consent screen nobody is looking at.
const loginTimeout = 5 * time.Minute

const successPage = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Signed in to pmon</title></head>
<body style="font-family: system-ui, sans-serif; padding: 3rem; text-align: center">
<h1>Signed in</h1>
<p>You can close this tab and return to Terraform.</p>
</body>
</html>`

// listenLoopback binds an ephemeral port on the loopback interface and reports the redirect URI
// that reaches it. The port is chosen at bind time rather than declared up front, which is why
// the client metadata document must not name one.
func listenLoopback() (net.Listener, string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("binding a loopback port for the OAuth redirect: %w", err)
	}
	redirect := (&url.URL{
		Scheme: "http",
		Host:   ln.Addr().String(),
		Path:   callbackPath,
	}).String()
	return ln, redirect, nil
}

// browserFetcher returns a fetcher that opens the authorization URL and waits on ln for the
// authorization server to redirect back.
func browserFetcher(ln net.Listener, notify func(string)) auth.AuthorizationCodeFetcher {
	return func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		ctx, cancel := context.WithTimeout(ctx, loginTimeout)
		defer cancel()

		type callback struct {
			result *auth.AuthorizationResult
			err    error
		}
		done := make(chan callback, 1)

		mux := http.NewServeMux()
		mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if desc := q.Get("error"); desc != "" {
				http.Error(w, "Authorization failed: "+desc, http.StatusBadRequest)
				done <- callback{err: fmt.Errorf("authorization server refused the request: %s", desc)}
				return
			}
			code := q.Get("code")
			if code == "" {
				http.Error(w, "Authorization failed: no code in the redirect", http.StatusBadRequest)
				done <- callback{err: errors.New("authorization server redirected without a code")}
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(successPage))
			done <- callback{result: &auth.AuthorizationResult{
				Code:  code,
				State: q.Get("state"),
				Iss:   q.Get("iss"),
			}}
		})

		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				done <- callback{err: fmt.Errorf("serving the OAuth redirect: %w", err)}
			}
		}()
		defer func() { _ = srv.Close() }()

		notify(args.URL)
		if err := openBrowser(args.URL); err != nil {
			notify("Could not open a browser automatically. Open the URL above by hand.")
		}

		select {
		case cb := <-done:
			return cb.result, cb.err
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for the pmon login to finish: %w", ctx.Err())
		}
	}
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

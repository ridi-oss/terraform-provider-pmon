// Command pmon-login authenticates against a pmon MCP endpoint and caches the token where the
// Terraform provider looks for it.
//
// The provider can do this itself on the first plan. It runs as a Terraform plugin though, and
// Terraform captures a plugin's stderr into its own log, so the authorization URL never reaches
// the terminal. That is invisible when the browser opens by itself and a five-minute stall when
// it does not. Logging in here first makes the URL visible and leaves the plan with nothing left
// to do.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
	"github.com/ridi-oss/terraform-provider-pmon/internal/provider"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "pmon-login:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, diags := provider.ConfigFromEnv(ctx)
	for _, d := range diags.Warnings() {
		fmt.Fprintf(os.Stderr, "pmon-login: %s: %s\n", d.Summary(), d.Detail())
	}
	if diags.HasError() {
		for _, d := range diags.Errors() {
			fmt.Fprintf(os.Stderr, "pmon-login: %s: %s\n", d.Summary(), d.Detail())
		}
		return errors.New("configuration is incomplete")
	}

	handler, err := provider.NewAuthHandler(cfg)
	if err != nil {
		return err
	}

	// Connecting performs the MCP initialize handshake, and that is what provokes the 401 the
	// login answers. A session that opens is proof the credential works, not just that a token
	// was issued.
	client, err := pmonmcp.Connect(ctx, pmonmcp.Options{
		Endpoint: cfg.Endpoint,
		OAuth:    handler,
		Version:  "pmon-login",
	})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	fmt.Printf("logged in to %s\n", cfg.Endpoint)
	if cfg.AccessToken != "" {
		fmt.Println("using the access token from the environment; nothing was cached")
		return nil
	}
	fmt.Printf("token cached at %s\n", cfg.TokenCachePath)
	return nil
}

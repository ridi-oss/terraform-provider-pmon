package provider

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories is used to instantiate a provider during acceptance testing.
// The factory function is called for each Terraform CLI command to create a provider
// server that the CLI can connect to and interact with.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"pmon": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	t.Helper()

	if v := os.Getenv(envEndpoint); v == "" {
		t.Fatalf("%s must be set for acceptance tests", envEndpoint)
	}
}

func TestResolveConfigDefaults(t *testing.T) {
	t.Setenv(envEndpoint, "https://pmon.example.com/mcp")
	t.Setenv(envClientMetadataURL, "")
	t.Setenv(envTokenCache, "")

	cfg, diags := resolveConfig(context.Background(), PmonProviderModel{
		Endpoint:          types.StringNull(),
		ClientMetadataURL: types.StringNull(),
		TokenCachePath:    types.StringNull(),
		Scopes:            types.ListNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if cfg.Endpoint != "https://pmon.example.com/mcp" {
		t.Errorf("endpoint from environment = %q", cfg.Endpoint)
	}
	if cfg.ClientMetadataURL != DefaultClientMetadataURL {
		t.Errorf("client metadata URL = %q, want the default", cfg.ClientMetadataURL)
	}
	if len(cfg.Scopes) != len(DefaultScopes) {
		t.Errorf("scopes = %v, want the four defaults", cfg.Scopes)
	}
}

// Config beats environment, so a checked-in provider block is never silently overridden by a
// stray variable in the operator's shell.
func TestResolveConfigPrefersConfigOverEnv(t *testing.T) {
	t.Setenv(envEndpoint, "https://from-env.example.com/mcp")

	cfg, diags := resolveConfig(context.Background(), PmonProviderModel{
		Endpoint:          types.StringValue("https://from-config.example.com/mcp"),
		ClientMetadataURL: types.StringNull(),
		TokenCachePath:    types.StringNull(),
		Scopes:            types.ListNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if cfg.Endpoint != "https://from-config.example.com/mcp" {
		t.Errorf("endpoint = %q, want the configured value", cfg.Endpoint)
	}
}

func TestResolveConfigRequiresEndpoint(t *testing.T) {
	t.Setenv(envEndpoint, "")

	_, diags := resolveConfig(context.Background(), PmonProviderModel{
		Endpoint:          types.StringNull(),
		ClientMetadataURL: types.StringNull(),
		TokenCachePath:    types.StringNull(),
		Scopes:            types.ListNull(types.StringType),
	})
	if !diags.HasError() {
		t.Fatal("expected a diagnostic when no endpoint is configured")
	}
}

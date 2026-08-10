package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func nullModel() PmonProviderModel {
	return PmonProviderModel{
		Endpoint:          types.StringNull(),
		ClientMetadataURL: types.StringNull(),
		TokenCachePath:    types.StringNull(),
		Scopes:            types.ListNull(types.StringType),
	}
}

func TestResolveConfigDefaults(t *testing.T) {
	t.Setenv(envEndpoint, "https://pmon.example.com/mcp")
	t.Setenv(envClientMetadataURL, "")
	t.Setenv(envTokenCache, "")

	cfg, diags := resolveConfig(context.Background(), nullModel())
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

	model := nullModel()
	model.Endpoint = types.StringValue("https://from-config.example.com/mcp")

	cfg, diags := resolveConfig(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if cfg.Endpoint != "https://from-config.example.com/mcp" {
		t.Errorf("endpoint = %q, want the configured value", cfg.Endpoint)
	}
}

func TestResolveConfigRequiresEndpoint(t *testing.T) {
	t.Setenv(envEndpoint, "")

	if _, diags := resolveConfig(context.Background(), nullModel()); !diags.HasError() {
		t.Fatal("expected a diagnostic when no endpoint is configured")
	}
}

func TestResolveConfigRejectsUnknownEndpoint(t *testing.T) {
	model := nullModel()
	model.Endpoint = types.StringUnknown()

	if _, diags := resolveConfig(context.Background(), model); !diags.HasError() {
		t.Fatal("expected a diagnostic for an endpoint that is not known at configure time")
	}
}

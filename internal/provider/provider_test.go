package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func nullModel() PmonProviderModel {
	return PmonProviderModel{
		Endpoint:          types.StringNull(),
		ClientMetadataURL: types.StringNull(),
		TokenCachePath:    types.StringNull(),
		Scopes:            types.SetNull(types.StringType),
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

func scopeSet(scopes ...string) types.Set {
	values := make([]attr.Value, 0, len(scopes))
	for _, scope := range scopes {
		values = append(values, types.StringValue(scope))
	}
	return types.SetValueMust(types.StringType, values)
}

// Omitting scopes has to stay the no-ceiling default, or every existing configuration would
// suddenly start refusing writes.
func TestResolveScopesDefaultsToNoCeiling(t *testing.T) {
	t.Setenv(envEndpoint, "https://pmon.example.com/mcp")
	t.Setenv(envScopes, "")

	cfg, diags := resolveConfig(context.Background(), nullModel())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(cfg.Scopes) != 0 {
		t.Errorf("scopes = %v, want none", cfg.Scopes)
	}
}

func TestResolveScopesFromEnv(t *testing.T) {
	t.Setenv(envEndpoint, "https://pmon.example.com/mcp")
	t.Setenv(envScopes, "mcp:read  mcp:identity:write")

	cfg, diags := resolveConfig(context.Background(), nullModel())
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got := strings.Join(cfg.Scopes, " "); got != "mcp:read mcp:identity:write" {
		t.Errorf("scopes = %q, want both scopes from the environment", got)
	}
}

func TestResolveScopesPrefersConfigOverEnv(t *testing.T) {
	t.Setenv(envEndpoint, "https://pmon.example.com/mcp")
	t.Setenv(envScopes, "mcp:identity:write")

	model := nullModel()
	model.Scopes = scopeSet("mcp:read")

	cfg, diags := resolveConfig(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got := strings.Join(cfg.Scopes, " "); got != "mcp:read" {
		t.Errorf("scopes = %q, want the configured value", got)
	}
}

// A misspelled scope would otherwise log in cleanly and then fail every call with
// insufficient_scope, which looks nothing like a typo.
func TestResolveScopesRejectsAnUnknownScope(t *testing.T) {
	t.Setenv(envEndpoint, "https://pmon.example.com/mcp")

	model := nullModel()
	model.Scopes = scopeSet("mcp:read", "mcp:identity")

	if _, diags := resolveConfig(context.Background(), model); !diags.HasError() {
		t.Fatal("expected a diagnostic for a scope pmon does not define")
	}
}

func TestResolveScopesRejectsAnEmptyList(t *testing.T) {
	t.Setenv(envEndpoint, "https://pmon.example.com/mcp")

	model := nullModel()
	model.Scopes = scopeSet()

	if _, diags := resolveConfig(context.Background(), model); !diags.HasError() {
		t.Fatal("expected a diagnostic: an empty list is the loosest ceiling, not the tightest")
	}
}

package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// DefaultClientMetadataURL identifies this provider to a pmon authorization server. pmon registers
// OAuth clients by fetching the document at the client_id URL (CIMD), so this names the provider
// itself, not any particular deployment.
const DefaultClientMetadataURL = "https://ridi-oss.github.io/terraform-provider-pmon/client-metadata.json"

// DefaultScopes is every scope the provider can need. Scopes are a consent ceiling, never a grant:
// pmon re-resolves the caller's roles and re-evaluates Cedar on every tool call. Narrowing this
// limits what a run may attempt; it can never widen authority.
var DefaultScopes = []string{
	"mcp:read",
	"mcp:datasources:write",
	"mcp:policies:write",
	"mcp:identity:write",
}

const (
	envEndpoint          = "PMON_ENDPOINT"
	envClientMetadataURL = "PMON_CLIENT_METADATA_URL"
	envTokenCache        = "PMON_TOKEN_CACHE"
)

var _ provider.Provider = &PmonProvider{}

// PmonProvider defines the provider implementation.
type PmonProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// PmonProviderModel describes the provider data model.
type PmonProviderModel struct {
	Endpoint          types.String `tfsdk:"endpoint"`
	ClientMetadataURL types.String `tfsdk:"client_metadata_url"`
	TokenCachePath    types.String `tfsdk:"token_cache_path"`
	Scopes            types.List   `tfsdk:"scopes"`
}

// Config is the provider configuration after environment fallbacks and defaults are applied.
type Config struct {
	Endpoint          string
	ClientMetadataURL string
	TokenCachePath    string
	Scopes            []string
}

func (p *PmonProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "pmon"
	resp.Version = p.version
}

func (p *PmonProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages proxy-monster access control -- Cedar policies, roles, groups, " +
			"and column classification -- through its MCP administration endpoint. " +
			"Authenticates with OAuth 2.1 (authorization code + PKCE), opening a browser on first use.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "pmon MCP endpoint, for example `https://pmon.example.com/mcp`. " +
					"May also be set with the `" + envEndpoint + "` environment variable.",
				Optional: true,
			},
			"client_metadata_url": schema.StringAttribute{
				MarkdownDescription: "HTTPS URL of the OAuth Client ID Metadata Document that identifies " +
					"this provider to pmon's authorization server. May also be set with the `" +
					envClientMetadataURL + "` environment variable. Defaults to `" +
					DefaultClientMetadataURL + "`.",
				Optional: true,
			},
			"token_cache_path": schema.StringAttribute{
				MarkdownDescription: "Where the OAuth token is cached between runs. May also be set with " +
					"the `" + envTokenCache + "` environment variable. Defaults to `~/.pmon/tf-token.json`.",
				Optional: true,
			},
			"scopes": schema.ListAttribute{
				MarkdownDescription: "OAuth scopes to request. A scope is a consent ceiling, not a grant: " +
					"pmon re-evaluates the caller's real authority with Cedar on every call, so narrowing " +
					"this can only restrict what a run may attempt. Defaults to all four scopes.",
				Optional:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func (p *PmonProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data PmonProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, diags := resolveConfig(ctx, data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.DataSourceData = cfg
	resp.ResourceData = cfg
}

func (p *PmonProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{}
}

func (p *PmonProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &PmonProvider{
			version: version,
		}
	}
}

func resolveConfig(ctx context.Context, data PmonProviderModel) (*Config, diag.Diagnostics) {
	var diags diag.Diagnostics

	// An unknown here means the value comes from another resource's output, which is not available
	// while the provider is being configured. Saying so beats failing later with an empty endpoint.
	for name, attr := range map[string]types.String{
		"endpoint":            data.Endpoint,
		"client_metadata_url": data.ClientMetadataURL,
		"token_cache_path":    data.TokenCachePath,
	} {
		if attr.IsUnknown() {
			diags.AddAttributeError(
				path.Root(name),
				"Unknown provider configuration",
				"The pmon provider cannot be configured with an unknown "+name+". Set it to a static "+
					"value or an input variable, or use its environment variable.",
			)
		}
	}
	if diags.HasError() {
		return nil, diags
	}

	cfg := &Config{
		Endpoint:          firstNonEmpty(data.Endpoint.ValueString(), os.Getenv(envEndpoint)),
		ClientMetadataURL: firstNonEmpty(data.ClientMetadataURL.ValueString(), os.Getenv(envClientMetadataURL), DefaultClientMetadataURL),
		TokenCachePath:    firstNonEmpty(data.TokenCachePath.ValueString(), os.Getenv(envTokenCache), defaultTokenCachePath()),
		Scopes:            DefaultScopes,
	}

	if cfg.Endpoint == "" {
		diags.AddAttributeError(
			path.Root("endpoint"),
			"Missing pmon endpoint",
			"Set the endpoint attribute on the provider block or the "+envEndpoint+" environment "+
				"variable to the pmon MCP endpoint, for example https://pmon.example.com/mcp.",
		)
	}

	if !data.Scopes.IsNull() && !data.Scopes.IsUnknown() {
		var scopes []string
		diags.Append(data.Scopes.ElementsAs(ctx, &scopes, false)...)
		if len(scopes) > 0 {
			cfg.Scopes = scopes
		}
	}

	return cfg, diags
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// defaultTokenCachePath keeps the cached OAuth token beside whatever else pmon tooling writes to
// the user's home. A home directory that cannot be resolved falls back to the working directory
// rather than failing configuration outright.
func defaultTokenCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".pmon-tf-token.json"
	}
	return filepath.Join(home, ".pmon", "tf-token.json")
}

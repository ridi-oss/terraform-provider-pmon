package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonauth"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

// DefaultClientMetadataURL identifies this provider to a pmon authorization server. pmon registers
// OAuth clients by fetching the document at the client_id URL (CIMD), so this names the provider
// itself, not any particular deployment.
const DefaultClientMetadataURL = "https://ridi-oss.github.io/terraform-provider-pmon/client-metadata.json"

// DefaultScopes is every scope pmon defines, and must match what client-metadata.json declares:
// pmon rejects an authorization request asking for anything the document does not list. It doubles
// as the allowed-value set for the provider's scopes argument, and is what a login asks for when
// that argument is omitted.
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
	envNoBrowser         = "PMON_NO_BROWSER"
	envScopes            = "PMON_SCOPES"
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
	Scopes            types.Set    `tfsdk:"scopes"`
}

// Config is the provider configuration after environment fallbacks and defaults are applied.
type Config struct {
	Endpoint          string
	ClientMetadataURL string
	TokenCachePath    string
	NoBrowser         bool
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
			"scopes": schema.SetAttribute{
				ElementType: types.StringType,
				MarkdownDescription: "Caps what a login may ask for. May also be set with the `" +
					envScopes + "` environment variable, space separated. Omit it to ask for whatever " +
					"pmon requires. Valid scopes are `" + strings.Join(DefaultScopes, "`, `") + "`.\n\n" +
					"A scope is a consent ceiling, never a grant: pmon re-resolves your roles and " +
					"re-evaluates Cedar on every call, so this can only ever subtract. Narrowing it makes " +
					"a write outside the list fail with `insufficient_scope` instead of prompting for " +
					"consent, which is how a read-only configuration stays read-only.",
				Optional: true,
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

	handler, err := pmonauth.NewHandler(pmonauth.Options{
		Endpoint:          cfg.Endpoint,
		ClientMetadataURL: cfg.ClientMetadataURL,
		CachePath:         cfg.TokenCachePath,
		NoBrowser:         cfg.NoBrowser,
		Scopes:            cfg.Scopes,
		Progress:          os.Stderr,
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot prepare pmon authentication", err.Error())
		return
	}

	// Connecting performs the MCP initialize handshake, which is what triggers the login. Doing it
	// here means one login per provider process, rather than whenever the first resource happens
	// to be read.
	client, err := pmonmcp.Connect(ctx, pmonmcp.Options{
		Endpoint: cfg.Endpoint,
		OAuth:    handler,
		Version:  p.version,
	})
	if err != nil {
		detail := err.Error()
		if errors.Is(err, pmonauth.ErrBrowserDisabled) {
			detail += "\n\nUnset " + envNoBrowser + " to log in with a browser. There is no non-interactive " +
				"credential for pmon's MCP endpoint yet, so an unattended run cannot authenticate."
		}
		resp.Diagnostics.AddError("Cannot reach pmon at "+cfg.Endpoint, detail)
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *PmonProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewPolicyResource,
		NewRoleResource,
		NewGroupResource,
		NewGroupRolesResource,
		NewGroupMemberResource,
		NewRoleAssignmentResource,
		NewMaskFnResource,
		NewColumnClassificationResource,
	}
}

func (p *PmonProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDatasourcesDataSource,
		NewPolicyDataSource,
		NewPolicySchemaDataSource,
		NewPoliciesDataSource,
		NewRolesDataSource,
		NewGroupsDataSource,
		NewUsersDataSource,
		NewCatalogDataSource,
		NewTableDetailDataSource,
		NewColumnTagsDataSource,
		NewDatasourceLivenessDataSource,
		NewDatasourceDataSource,
		NewRoleDataSource,
		NewGroupDataSource,
		NewUserDataSource,
	}
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
	if data.Scopes.IsUnknown() {
		diags.AddAttributeError(
			path.Root("scopes"),
			"Unknown provider configuration",
			"The pmon provider cannot be configured with unknown scopes. Set them to static values "+
				"or an input variable, or use the "+envScopes+" environment variable.",
		)
	}
	if diags.HasError() {
		return nil, diags
	}

	scopes, scopeDiags := resolveScopes(ctx, data.Scopes)
	diags.Append(scopeDiags...)
	if diags.HasError() {
		return nil, diags
	}

	cfg := &Config{
		Endpoint:          firstNonEmpty(data.Endpoint.ValueString(), os.Getenv(envEndpoint)),
		ClientMetadataURL: firstNonEmpty(data.ClientMetadataURL.ValueString(), os.Getenv(envClientMetadataURL), DefaultClientMetadataURL),
		TokenCachePath:    firstNonEmpty(data.TokenCachePath.ValueString(), os.Getenv(envTokenCache), defaultTokenCachePath()),
		NoBrowser:         os.Getenv(envNoBrowser) != "",
		Scopes:            scopes,
	}

	if cfg.Endpoint == "" {
		diags.AddAttributeError(
			path.Root("endpoint"),
			"Missing pmon endpoint",
			"Set the endpoint attribute on the provider block or the "+envEndpoint+" environment "+
				"variable to the pmon MCP endpoint, for example https://pmon.example.com/mcp.",
		)
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

// resolveScopes reads the scope ceiling from configuration or the environment, rejecting anything
// pmon does not define. A typo would otherwise become a login that succeeds and then fails every
// call with insufficient_scope, which reads nothing like a misspelling.
func resolveScopes(ctx context.Context, configured types.Set) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics

	// An explicitly empty list reads like the tightest possible ceiling but would be the loosest,
	// since no ceiling is how "unset" is spelled. Refuse it rather than silently invert it.
	if !configured.IsNull() && len(configured.Elements()) == 0 {
		diags.AddAttributeError(
			path.Root("scopes"),
			"Empty scopes",
			"An empty scopes list leaves no ceiling at all rather than forbidding everything. Remove "+
				"the argument to ask for whatever pmon requires, or name the scopes to allow.",
		)
		return nil, diags
	}

	var raw []string
	if configured.IsNull() {
		raw = strings.FieldsFunc(os.Getenv(envScopes), func(r rune) bool {
			return unicode.IsSpace(r) || r == ','
		})
	} else {
		diags.Append(configured.ElementsAs(ctx, &raw, false)...)
		if diags.HasError() {
			return nil, diags
		}
	}

	scopes := make([]string, 0, len(raw))
	for _, scope := range raw {
		scope = strings.TrimSpace(scope)
		switch {
		case scope == "":
			continue
		case !slices.Contains(DefaultScopes, scope):
			diags.AddAttributeError(
				path.Root("scopes"),
				"Unknown pmon scope",
				strconv.Quote(scope)+" is not a scope pmon defines. Valid scopes are "+
					strings.Join(DefaultScopes, ", ")+".",
			)
		case !slices.Contains(scopes, scope):
			scopes = append(scopes, scope)
		}
	}
	return scopes, diags
}

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &policyDataSource{}
	_ datasource.DataSourceWithConfigure = &policyDataSource{}
)

// NewPolicyDataSource returns the data source reading one Cedar policy by name.
func NewPolicyDataSource() datasource.DataSource { return &policyDataSource{} }

type policyDataSource struct {
	client *pmonmcp.Client
}

type policyDataSourceModel struct {
	Name      types.String `tfsdk:"name"`
	ID        types.Int64  `tfsdk:"id"`
	Origin    types.String `tfsdk:"origin"`
	SystemKey types.String `tfsdk:"system_key"`
	CedarSrc  types.String `tfsdk:"cedar_src"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	UpdatedBy types.String `tfsdk:"updated_by"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func (d *policyDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy"
}

func (d *policyDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One Cedar policy, by name. Useful for reading a shipped `system:` policy, " +
			"which cannot be managed as a resource.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Policy name.",
				Required:            true,
			},
			"id": schema.Int64Attribute{
				MarkdownDescription: "pmon's internal id. Shipped policies have negative ids.",
				Computed:            true,
			},
			"origin": schema.StringAttribute{
				MarkdownDescription: "`SYSTEM` for a shipped policy, which is immutable, or `USER`.",
				Computed:            true,
			},
			"system_key": schema.StringAttribute{
				MarkdownDescription: "Stable key identifying a shipped policy across upgrades. Null for user policies.",
				Computed:            true,
			},
			"cedar_src": schema.StringAttribute{
				MarkdownDescription: "The Cedar source.",
				Computed:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the policy takes part in decisions.",
				Computed:            true,
			},
			"updated_by": schema.StringAttribute{
				MarkdownDescription: "Principal that last wrote the policy.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "When the policy was last written.",
				Computed:            true,
			},
		},
	}
}

func (d *policyDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *policyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	var state policyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy, err := pmonmcp.Call[pmonmcp.Policy](ctx, d.client, "get_policy", map[string]any{
		"name": state.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot read the pmon policy "+state.Name.ValueString(), err.Error())
		return
	}

	state.ID = types.Int64Value(policy.ID)
	state.Origin = types.StringValue(policy.Origin)
	state.SystemKey = stringOrNull(policy.SystemKey)
	state.CedarSrc = types.StringValue(policy.CedarSrc)
	state.Enabled = types.BoolValue(policy.Enabled)
	state.UpdatedBy = stringOrNull(policy.UpdatedBy)
	state.UpdatedAt = stringOrNull(policy.UpdatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &policiesDataSource{}
	_ datasource.DataSourceWithConfigure = &policiesDataSource{}
)

// NewPoliciesDataSource returns the data source listing every Cedar policy.
func NewPoliciesDataSource() datasource.DataSource { return &policiesDataSource{} }

type policiesDataSource struct {
	client *pmonmcp.Client
}

type policyEntryModel struct {
	ID        types.Int64  `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Origin    types.String `tfsdk:"origin"`
	SystemKey types.String `tfsdk:"system_key"`
	CedarSrc  types.String `tfsdk:"cedar_src"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	UpdatedBy types.String `tfsdk:"updated_by"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

type policiesModel struct {
	Policies []policyEntryModel `tfsdk:"policies"`
}

func (d *policiesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policies"
}

func (d *policiesDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every Cedar policy, shipped and authored alike.\n\n" +
			"Cedar evaluates all of them on every decision and a `forbid` always beats a `permit`, so " +
			"reading the whole set is the only way to reason about what a new policy will actually do. " +
			"Filter on `origin` to separate the shipped `SYSTEM` presets from your own.",
		Attributes: map[string]schema.Attribute{
			"policies": schema.ListNestedAttribute{
				MarkdownDescription: "The policies, as pmon reports them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							MarkdownDescription: "pmon's internal id. Shipped policies have negative ids.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Policy name.",
							Computed:            true,
						},
						"origin": schema.StringAttribute{
							MarkdownDescription: "`SYSTEM` for a shipped policy, which is immutable, or `USER`.",
							Computed:            true,
						},
						"system_key": schema.StringAttribute{
							MarkdownDescription: "Stable key identifying a shipped policy across upgrades. " +
								"Null for authored policies.",
							Computed: true,
						},
						"cedar_src": schema.StringAttribute{
							MarkdownDescription: "The Cedar source.",
							Computed:            true,
						},
						"enabled": schema.BoolAttribute{
							MarkdownDescription: "Whether the policy takes part in decisions. A disabled " +
								"policy is inert but still listed.",
							Computed: true,
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
				},
			},
		},
	}
}

func (d *policiesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *policiesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	found, err := pmonmcp.Call[[]pmonmcp.Policy](ctx, d.client, "list_policies", map[string]any{})
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon policies", err.Error())
		return
	}

	state := policiesModel{Policies: make([]policyEntryModel, 0, len(found))}
	for _, policy := range found {
		state.Policies = append(state.Policies, policyEntryModel{
			ID:        types.Int64Value(policy.ID),
			Name:      types.StringValue(policy.Name),
			Origin:    types.StringValue(policy.Origin),
			SystemKey: stringOrNull(policy.SystemKey),
			CedarSrc:  types.StringValue(policy.CedarSrc),
			Enabled:   types.BoolValue(policy.Enabled),
			UpdatedBy: stringOrNull(policy.UpdatedBy),
			UpdatedAt: stringOrNull(policy.UpdatedAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

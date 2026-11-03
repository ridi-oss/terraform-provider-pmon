package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &workloadBindingsDataSource{}
	_ datasource.DataSourceWithConfigure = &workloadBindingsDataSource{}
)

func NewWorkloadBindingsDataSource() datasource.DataSource { return &workloadBindingsDataSource{} }

type workloadBindingsDataSource struct {
	client *pmonmcp.Client
}

type workloadBindingEntryModel struct {
	ID          types.Int64  `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Issuer      types.String `tfsdk:"issuer"`
	Subject     types.String `tfsdk:"subject"`
	Conditions  types.Map    `tfsdk:"conditions"`
	Principal   types.String `tfsdk:"principal"`
	Purposes    types.Set    `tfsdk:"purposes"`
	MCPScopes   types.Set    `tfsdk:"mcp_scopes"`
	TTLSeconds  types.Int64  `tfsdk:"ttl_seconds"`
	Enabled     types.Bool   `tfsdk:"enabled"`
}

type workloadBindingsModel struct {
	Bindings []workloadBindingEntryModel `tfsdk:"bindings"`
}

func (d *workloadBindingsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workload_bindings"
}

func (d *workloadBindingsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: description, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every workload binding: which workload tokens map to which service principal.",
		Attributes: map[string]schema.Attribute{
			"bindings": schema.ListNestedAttribute{
				MarkdownDescription: "The bindings, as pmon reports them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.Int64Attribute{MarkdownDescription: "pmon's internal id.", Computed: true},
						"name":        computed("Binding name."),
						"description": computed("What the workload is."),
						"issuer":      computed("Trusted issuer name."),
						"subject":     computed("Exact `sub`, or a prefix ending in `*`."),
						"conditions": schema.MapAttribute{ElementType: conditionValuesType, Computed: true,
							MarkdownDescription: "Claims that must also hold."},
						"principal": computed("Service principal the token acts as."),
						"purposes": schema.SetAttribute{ElementType: types.StringType, Computed: true,
							MarkdownDescription: "What an exchange may mint."},
						"mcp_scopes": schema.SetAttribute{ElementType: types.StringType, Computed: true,
							MarkdownDescription: "The most an `mcp` exchange may carry."},
						"ttl_seconds": schema.Int64Attribute{MarkdownDescription: "Lifetime of a minted token.", Computed: true},
						"enabled":     schema.BoolAttribute{MarkdownDescription: "Whether the binding matches tokens.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *workloadBindingsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *workloadBindingsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}
	listed, err := pmonmcp.Call[pmonmcp.WorkloadBindings](ctx, d.client, "list_workload_bindings", map[string]any{})
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon workload bindings", err.Error())
		return
	}
	state := workloadBindingsModel{Bindings: make([]workloadBindingEntryModel, 0, len(listed.Bindings))}
	for _, b := range listed.Bindings {
		var m workloadBindingModel
		m.Conditions = types.MapUnknown(conditionValuesType)
		resp.Diagnostics.Append(applyWorkloadBinding(ctx, &m, b)...)
		state.Bindings = append(state.Bindings, workloadBindingEntryModel(m))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

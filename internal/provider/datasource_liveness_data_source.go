package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &datasourceLivenessDataSource{}
	_ datasource.DataSourceWithConfigure = &datasourceLivenessDataSource{}
)

// NewDatasourceLivenessDataSource returns the data source reporting proxy attachment.
func NewDatasourceLivenessDataSource() datasource.DataSource {
	return &datasourceLivenessDataSource{}
}

type datasourceLivenessDataSource struct {
	client *pmonmcp.Client
}

type datasourceLivenessModel struct {
	Datasource      types.String `tfsdk:"datasource"`
	Attached        types.Bool   `tfsdk:"attached"`
	CatalogSyncedAt types.String `tfsdk:"catalog_synced_at"`
	LastSeenAt      types.String `tfsdk:"last_seen_at"`
}

func (d *datasourceLivenessDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datasource_liveness"
}

func (d *datasourceLivenessDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Whether a proxy is currently attached to a datasource.\n\n" +
			"A datasource stays registered after its proxy goes away, so a row in `pmon_datasources` " +
			"is not evidence anything is serving it. Assert on `attached` in a `lifecycle` precondition " +
			"where a change only makes sense against a live datasource.",
		Attributes: map[string]schema.Attribute{
			"datasource": schema.StringAttribute{
				MarkdownDescription: "Datasource to check.",
				Required:            true,
			},
			"attached": schema.BoolAttribute{
				MarkdownDescription: "Whether a proxy is attached right now.",
				Computed:            true,
			},
			"catalog_synced_at": schema.StringAttribute{
				MarkdownDescription: "When a proxy last pushed a catalog. Null means it never has.",
				Computed:            true,
			},
			"last_seen_at": schema.StringAttribute{
				MarkdownDescription: "When a proxy last checked in.",
				Computed:            true,
			},
		},
	}
}

func (d *datasourceLivenessDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *datasourceLivenessDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	var state datasourceLivenessModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	liveness, err := pmonmcp.Call[pmonmcp.DatasourceLiveness](ctx, d.client, "get_datasource_liveness", map[string]any{
		"datasource": state.Datasource.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot check liveness of "+state.Datasource.ValueString(), err.Error())
		return
	}

	state.Attached = types.BoolValue(liveness.Attached)
	state.CatalogSyncedAt = stringOrNull(liveness.CatalogSyncedAt)
	state.LastSeenAt = stringOrNull(liveness.LastSeenAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

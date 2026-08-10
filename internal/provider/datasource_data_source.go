package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &datasourceDataSource{}
	_ datasource.DataSourceWithConfigure = &datasourceDataSource{}
)

// NewDatasourceDataSource returns the data source reading one brokered datasource.
func NewDatasourceDataSource() datasource.DataSource { return &datasourceDataSource{} }

type datasourceDataSource struct {
	client *pmonmcp.Client
}

type datasourceDataSourceModel struct {
	Name            types.String `tfsdk:"name"`
	ID              types.Int64  `tfsdk:"id"`
	Engine          types.String `tfsdk:"engine"`
	Host            types.String `tfsdk:"host"`
	Port            types.Int64  `tfsdk:"port"`
	DBName          types.String `tfsdk:"db_name"`
	Tags            []string     `tfsdk:"tags"`
	DefaultSchemas  []string     `tfsdk:"default_schemas"`
	EngineVersion   types.String `tfsdk:"engine_version"`
	CatalogSyncedAt types.String `tfsdk:"catalog_synced_at"`
	LastSeenAt      types.String `tfsdk:"last_seen_at"`
}

func (d *datasourceDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datasource"
}

func (d *datasourceDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One brokered datasource, by name.\n\n" +
			"Datasources cannot be managed by this provider: the proxy registers itself over gRPC and " +
			"pushes its own tags from `PM_DATASOURCE_TAGS`. Reading one is how a configuration asserts, " +
			"in a `lifecycle` precondition, that a tag its policy keys on is actually present.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Datasource name. This is what Cedar policies match on.",
				Required:            true,
			},
			"id": schema.Int64Attribute{
				MarkdownDescription: "pmon's internal id.",
				Computed:            true,
			},
			"engine": schema.StringAttribute{
				MarkdownDescription: "Database engine, `mysql` or `postgres`.",
				Computed:            true,
			},
			"host": schema.StringAttribute{
				MarkdownDescription: "Target host. Descriptive only; nothing in enforcement reads it.",
				Computed:            true,
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "Target port. Descriptive only.",
				Computed:            true,
			},
			"db_name": schema.StringAttribute{
				MarkdownDescription: "Target database name. Descriptive only.",
				Computed:            true,
			},
			"tags": schema.ListAttribute{
				MarkdownDescription: "Tags the proxy pushed at registration. The posture tags " +
					"`system:production` and `system:development` are what the shipped presets key on.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"default_schemas": schema.ListAttribute{
				MarkdownDescription: "Schemas resolved when a query does not qualify a table.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"engine_version": schema.StringAttribute{
				MarkdownDescription: "Engine version the proxy last reported.",
				Computed:            true,
			},
			"catalog_synced_at": schema.StringAttribute{
				MarkdownDescription: "When the proxy last pushed a catalog. Null means it never has, " +
					"which leaves the datasource with no columns to classify.",
				Computed: true,
			},
			"last_seen_at": schema.StringAttribute{
				MarkdownDescription: "When a proxy last checked in for this datasource.",
				Computed:            true,
			},
		},
	}
}

func (d *datasourceDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *datasourceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	var state datasourceDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := state.Name.ValueString()
	found, exists, err := d.client.Listings().Datasource(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon datasources", err.Error())
		return
	}
	if !exists {
		resp.Diagnostics.AddError(
			"No such pmon datasource",
			notFoundDetail("datasource", name, "pmon_datasources"),
		)
		return
	}

	state.ID = types.Int64Value(found.ID)
	state.Engine = types.StringValue(found.Engine)
	state.Host = types.StringValue(found.Host)
	state.Port = types.Int64Value(found.Port)
	state.DBName = types.StringValue(found.DBName)
	state.Tags = orEmpty(found.Tags)
	state.DefaultSchemas = orEmpty(found.DefaultSchemas)
	state.EngineVersion = stringOrNull(found.EngineVersion)
	state.CatalogSyncedAt = stringOrNull(found.CatalogSyncedAt)
	state.LastSeenAt = stringOrNull(found.LastSeenAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &datasourcesDataSource{}
	_ datasource.DataSourceWithConfigure = &datasourcesDataSource{}
)

// NewDatasourcesDataSource returns the data source listing brokered datasources.
func NewDatasourcesDataSource() datasource.DataSource { return &datasourcesDataSource{} }

type datasourcesDataSource struct {
	client *pmonmcp.Client
}

type datasourceEntryModel struct {
	ID              types.Int64  `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
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

type datasourcesModel struct {
	Datasources []datasourceEntryModel `tfsdk:"datasources"`
}

func (d *datasourcesDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datasources"
}

func (d *datasourcesDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every datasource pmon brokers.\n\n" +
			"Datasources cannot be managed by this provider: the proxy registers itself over gRPC and " +
			"pushes its own tags from `PM_DATASOURCE_TAGS`, so there is no tool to create one or to " +
			"change a tag. Read them here to assert, with a `lifecycle` precondition, that a tag a " +
			"policy depends on is actually present before that policy is applied.",
		Attributes: map[string]schema.Attribute{
			"datasources": schema.ListNestedAttribute{
				MarkdownDescription: "The brokered datasources, as pmon reports them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							MarkdownDescription: "pmon's internal id. Cedar keys on the name, not this.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Logical name. This is what Cedar policies match on.",
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
								"`system:production` and `system:development` are what the shipped preset " +
								"policies key on; an untagged datasource falls back to the production floor.",
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
							MarkdownDescription: "When the proxy last pushed a catalog. Null means it never " +
								"has, which leaves the datasource with no columns to classify.",
							Computed: true,
						},
						"last_seen_at": schema.StringAttribute{
							MarkdownDescription: "When a proxy last checked in for this datasource.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *datasourcesDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *datasourcesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	found, err := pmonmcp.Call[[]pmonmcp.Datasource](ctx, d.client, "list_datasources", map[string]any{})
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon datasources", err.Error())
		return
	}

	state := datasourcesModel{Datasources: make([]datasourceEntryModel, 0, len(found))}
	for _, ds := range found {
		state.Datasources = append(state.Datasources, datasourceEntryModel{
			ID:              types.Int64Value(ds.ID),
			Name:            types.StringValue(ds.Name),
			Engine:          types.StringValue(ds.Engine),
			Host:            types.StringValue(ds.Host),
			Port:            types.Int64Value(ds.Port),
			DBName:          types.StringValue(ds.DBName),
			Tags:            orEmpty(ds.Tags),
			DefaultSchemas:  orEmpty(ds.DefaultSchemas),
			EngineVersion:   stringOrNull(ds.EngineVersion),
			CatalogSyncedAt: stringOrNull(ds.CatalogSyncedAt),
			LastSeenAt:      stringOrNull(ds.LastSeenAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// stringOrNull keeps an omitted field null rather than collapsing it to "". A datasource that has
// never synced a catalog is a different state from one that synced at an unknown time.
func stringOrNull(value *string) types.String {
	if value == nil {
		return types.StringNull()
	}
	return types.StringValue(*value)
}

// orEmpty normalises a nil slice to an empty one, so the attribute is an empty list rather than
// null and `for` expressions over it do not have to guard.
func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

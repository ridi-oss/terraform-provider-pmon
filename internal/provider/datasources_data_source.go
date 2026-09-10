package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
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
			"datasources": schema.ListAttribute{
				MarkdownDescription: "The brokered datasources, as pmon reports them. Each object contains " +
					"`id` (internal id), `name` (the Cedar identity), `engine` (`mysql` or `postgres`), " +
					"`host`, `port`, `db_name`, `tags` (strings pushed by the proxy), " +
					"`default_schemas` (strings used to resolve unqualified tables), " +
					"`engine_version`, `catalog_synced_at`, and `last_seen_at`. " +
					"The final three fields are nullable; a null `catalog_synced_at` means the proxy " +
					"has never pushed a catalog. Host, port, and database name are descriptive only. " +
					"The shipped policies use `system:production` and `system:development` tags; " +
					"an untagged datasource falls back to the production floor.",
				Computed: true,
				ElementType: types.ObjectType{AttrTypes: map[string]attr.Type{
					"id":                types.Int64Type,
					"name":              types.StringType,
					"engine":            types.StringType,
					"host":              types.StringType,
					"port":              types.Int64Type,
					"db_name":           types.StringType,
					"tags":              types.ListType{ElemType: types.StringType},
					"default_schemas":   types.ListType{ElemType: types.StringType},
					"engine_version":    types.StringType,
					"catalog_synced_at": types.StringType,
					"last_seen_at":      types.StringType,
				}},
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

package provider

import (
	"context"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

// systemSchemas are the engine's own bookkeeping. Nobody classifies them, and they are a large
// share of any catalog: information_schema alone is 800 of example-prod-ro's 2000 rows.
var systemSchemas = []string{
	"information_schema",
	"performance_schema",
	"mysql",
	"sys",
	"pg_catalog",
	"pg_toast",
}

var (
	_ datasource.DataSource              = &catalogDataSource{}
	_ datasource.DataSourceWithConfigure = &catalogDataSource{}
)

// NewCatalogDataSource returns the data source listing a datasource's columns.
func NewCatalogDataSource() datasource.DataSource { return &catalogDataSource{} }

type catalogDataSource struct {
	client *pmonmcp.Client
}

type catalogColumnModel struct {
	Schema   types.String `tfsdk:"schema"`
	Table    types.String `tfsdk:"table"`
	Column   types.String `tfsdk:"column"`
	DataType types.String `tfsdk:"data_type"`
	SQLType  types.String `tfsdk:"sql_type"`
	Ordinal  types.Int64  `tfsdk:"ordinal"`
	Nullable types.Bool   `tfsdk:"nullable"`
	Tags     []string     `tfsdk:"tags"`
}

type catalogModel struct {
	Datasource           types.String         `tfsdk:"datasource"`
	Schema               types.String         `tfsdk:"schema"`
	Table                types.String         `tfsdk:"table"`
	IncludeSystemSchemas types.Bool           `tfsdk:"include_system_schemas"`
	Columns              []catalogColumnModel `tfsdk:"columns"`
}

func (d *catalogDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_catalog"
}

func (d *catalogDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every column pmon knows about in a datasource, with whatever tags it " +
			"already carries.\n\n" +
			"This is what lets a classification be derived rather than typed out: a `for` expression " +
			"over these columns feeds `pmon_column_classification` directly.\n\n" +
			"**Filter it.** pmon returns the whole catalog in one response -- 2000 columns for a " +
			"mid-sized database -- and everything that survives the filter lands in Terraform state. " +
			"Narrow with `schema`, and with `table` where you can.",
		Attributes: map[string]schema.Attribute{
			"datasource": schema.StringAttribute{
				MarkdownDescription: "Datasource to read.",
				Required:            true,
			},
			"schema": schema.StringAttribute{
				MarkdownDescription: "Keep only columns in this schema.",
				Optional:            true,
			},
			"table": schema.StringAttribute{
				MarkdownDescription: "Keep only columns in this table.",
				Optional:            true,
			},
			"include_system_schemas": schema.BoolAttribute{
				MarkdownDescription: "Keep the engine's own schemas (`information_schema`, " +
					"`performance_schema`, `mysql`, `sys`, `pg_catalog`, `pg_toast`). Defaults to " +
					"`false`, because they are never classified and are a large share of any catalog.",
				Optional: true,
			},
			"columns": schema.ListNestedAttribute{
				MarkdownDescription: "The matching columns, in the order pmon reported them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"schema": schema.StringAttribute{
							MarkdownDescription: "Schema holding the table.",
							Computed:            true,
						},
						"table": schema.StringAttribute{
							MarkdownDescription: "Table holding the column.",
							Computed:            true,
						},
						"column": schema.StringAttribute{
							MarkdownDescription: "Column name.",
							Computed:            true,
						},
						"data_type": schema.StringAttribute{
							MarkdownDescription: "Engine's own type name, for example `varchar`.",
							Computed:            true,
						},
						"sql_type": schema.StringAttribute{
							MarkdownDescription: "Normalised SQL type, for example `VARCHAR`.",
							Computed:            true,
						},
						"ordinal": schema.Int64Attribute{
							MarkdownDescription: "Position of the column in the table, from 1.",
							Computed:            true,
						},
						"nullable": schema.BoolAttribute{
							MarkdownDescription: "Whether the column accepts null.",
							Computed:            true,
						},
						"tags": schema.ListAttribute{
							MarkdownDescription: "Tags already on the column. Empty when unclassified.",
							Computed:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
		},
	}
}

func (d *catalogDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *catalogDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	var state catalogModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := pmonmcp.Call[[]pmonmcp.CatalogColumn](ctx, d.client, "browse_catalog", map[string]any{
		"datasource": state.Datasource.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot browse the catalog of "+state.Datasource.ValueString(), err.Error())
		return
	}

	filter := catalogFilter{
		Schema:               state.Schema.ValueString(),
		Table:                state.Table.ValueString(),
		IncludeSystemSchemas: state.IncludeSystemSchemas.ValueBool(),
	}

	state.Columns = make([]catalogColumnModel, 0, len(found))
	for _, column := range found {
		if !filter.keep(column) {
			continue
		}
		state.Columns = append(state.Columns, catalogColumnModel{
			Schema:   types.StringValue(column.Schema),
			Table:    types.StringValue(column.Table),
			Column:   types.StringValue(column.Column),
			DataType: types.StringValue(column.DataType),
			SQLType:  types.StringValue(column.SQLType),
			Ordinal:  types.Int64Value(column.Ordinal),
			Nullable: types.BoolValue(column.Nullable),
			Tags:     classificationTags(column.Classification),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// catalogFilter narrows what reaches state. browse_catalog takes no filter arguments, so this
// runs over the response rather than the request.
type catalogFilter struct {
	Schema               string
	Table                string
	IncludeSystemSchemas bool
}

func (f catalogFilter) keep(column pmonmcp.CatalogColumn) bool {
	if f.Schema != "" && column.Schema != f.Schema {
		return false
	}
	if f.Table != "" && column.Table != f.Table {
		return false
	}
	// An explicitly named system schema is honoured: asking for information_schema by name and
	// getting nothing back would be its own kind of surprise.
	if !f.IncludeSystemSchemas && f.Schema == "" && slices.Contains(systemSchemas, column.Schema) {
		return false
	}
	return true
}

func classificationTags(classification *pmonmcp.Classification) []string {
	if classification == nil || classification.Tags == nil {
		return []string{}
	}
	return classification.Tags
}

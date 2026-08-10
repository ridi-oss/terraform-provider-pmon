package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &columnTagsDataSource{}
	_ datasource.DataSourceWithConfigure = &columnTagsDataSource{}
)

// NewColumnTagsDataSource returns the data source listing a datasource's classified columns.
func NewColumnTagsDataSource() datasource.DataSource { return &columnTagsDataSource{} }

type columnTagsDataSource struct {
	client *pmonmcp.Client
}

type columnTagEntryModel struct {
	Schema types.String `tfsdk:"schema"`
	Table  types.String `tfsdk:"table"`
	Column types.String `tfsdk:"column"`
	Tags   []string     `tfsdk:"tags"`
}

type columnTagsModel struct {
	Datasource types.String          `tfsdk:"datasource"`
	Columns    []columnTagEntryModel `tfsdk:"columns"`
}

func (d *columnTagsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_column_tags"
}

func (d *columnTagsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Only the columns of a datasource that carry tags.\n\n" +
			"An empty result on a production datasource is worth noticing: the shipped presets read " +
			"every column `unless resource in Tag::\"pii\"`, so with nothing tagged that exclusion never " +
			"fires and the whole datasource is readable in cleartext.\n\n" +
			"Mask functions are not reported here; pmon's read tools do not expose which one a column " +
			"carries.",
		Attributes: map[string]schema.Attribute{
			"datasource": schema.StringAttribute{
				MarkdownDescription: "Datasource to read.",
				Required:            true,
			},
			"columns": schema.ListNestedAttribute{
				MarkdownDescription: "The classified columns.",
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
						"tags": schema.ListAttribute{
							MarkdownDescription: "Tags on the column.",
							Computed:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
		},
	}
}

func (d *columnTagsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *columnTagsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	var state columnTagsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := pmonmcp.Call[[]pmonmcp.ColumnTag](ctx, d.client, "list_column_tags", map[string]any{
		"datasource": state.Datasource.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot read column tags of "+state.Datasource.ValueString(), err.Error())
		return
	}

	state.Columns = make([]columnTagEntryModel, 0, len(found))
	for _, column := range found {
		state.Columns = append(state.Columns, columnTagEntryModel{
			Schema: types.StringValue(column.Schema),
			Table:  types.StringValue(column.Table),
			Column: types.StringValue(column.Column),
			Tags:   orEmpty(column.Tags),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

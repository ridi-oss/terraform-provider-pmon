package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ datasource.DataSource              = &tableDetailDataSource{}
	_ datasource.DataSourceWithConfigure = &tableDetailDataSource{}
)

// NewTableDetailDataSource returns the data source describing one table.
func NewTableDetailDataSource() datasource.DataSource { return &tableDetailDataSource{} }

type tableDetailDataSource struct {
	client *pmonmcp.Client
}

type detailColumnModel struct {
	Name          types.String `tfsdk:"name"`
	DataType      types.String `tfsdk:"data_type"`
	Ordinal       types.Int64  `tfsdk:"ordinal"`
	Nullable      types.Bool   `tfsdk:"nullable"`
	DefaultValue  types.String `tfsdk:"default_value"`
	PartOfIndex   types.Bool   `tfsdk:"part_of_index"`
	AutoIncrement types.Bool   `tfsdk:"auto_increment"`
	Comment       types.String `tfsdk:"comment"`
	Tags          []string     `tfsdk:"tags"`
}

type foreignKeyModel struct {
	Name          types.String `tfsdk:"name"`
	SourceSchema  types.String `tfsdk:"source_schema"`
	SourceTable   types.String `tfsdk:"source_table"`
	SourceColumns []string     `tfsdk:"source_columns"`
	TargetSchema  types.String `tfsdk:"target_schema"`
	TargetTable   types.String `tfsdk:"target_table"`
	TargetColumns []string     `tfsdk:"target_columns"`
}

type tableDetailModel struct {
	Datasource    types.String        `tfsdk:"datasource"`
	Schema        types.String        `tfsdk:"schema"`
	Table         types.String        `tfsdk:"table"`
	Columns       []detailColumnModel `tfsdk:"columns"`
	ForeignKeys   []foreignKeyModel   `tfsdk:"foreign_keys"`
	ReferencedBy  []foreignKeyModel   `tfsdk:"referenced_by"`
	Engine        types.String        `tfsdk:"engine"`
	EstimatedRows types.Int64         `tfsdk:"estimated_rows"`
	OnDiskBytes   types.Int64         `tfsdk:"on_disk_bytes"`
}

func (d *tableDetailDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_table_detail"
}

func (d *tableDetailDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One table's live shape: columns with their current tags, plus the " +
			"relationships and size pmon sees.\n\n" +
			"Use this over `pmon_catalog` when the decision is about one table. The foreign keys are " +
			"worth reading before classifying: a column that is a key into a table holding PII usually " +
			"deserves the same treatment as the column it points at.",
		Attributes: map[string]schema.Attribute{
			"datasource": schema.StringAttribute{
				MarkdownDescription: "Datasource holding the table.",
				Required:            true,
			},
			"schema": schema.StringAttribute{
				MarkdownDescription: "Schema holding the table.",
				Required:            true,
			},
			"table": schema.StringAttribute{
				MarkdownDescription: "Table to describe.",
				Required:            true,
			},
			"engine": schema.StringAttribute{
				MarkdownDescription: "Storage engine, for example `InnoDB`.",
				Computed:            true,
			},
			"estimated_rows": schema.Int64Attribute{
				MarkdownDescription: "The engine's own row estimate. An estimate, not a count.",
				Computed:            true,
			},
			"on_disk_bytes": schema.Int64Attribute{
				MarkdownDescription: "Approximate size on disk.",
				Computed:            true,
			},
			"columns": schema.ListNestedAttribute{
				MarkdownDescription: "The table's columns, in ordinal order.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Column name.",
							Computed:            true,
						},
						"data_type": schema.StringAttribute{
							MarkdownDescription: "Engine's own type name.",
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
						"default_value": schema.StringAttribute{
							MarkdownDescription: "Declared default, null when there is none.",
							Computed:            true,
						},
						"part_of_index": schema.BoolAttribute{
							MarkdownDescription: "Whether any index covers the column.",
							Computed:            true,
						},
						"auto_increment": schema.BoolAttribute{
							MarkdownDescription: "Whether the engine assigns the value.",
							Computed:            true,
						},
						"comment": schema.StringAttribute{
							MarkdownDescription: "Column comment. Often the best hint about what a column " +
								"actually holds.",
							Computed: true,
						},
						"tags": schema.ListAttribute{
							MarkdownDescription: "Tags already on the column. Empty when unclassified.",
							Computed:            true,
							ElementType:         types.StringType,
						},
					},
				},
			},
			"foreign_keys": schema.ListNestedAttribute{
				MarkdownDescription: "Keys this table holds into other tables.",
				Computed:            true,
				NestedObject:        schema.NestedAttributeObject{Attributes: foreignKeyAttributes()},
			},
			"referenced_by": schema.ListNestedAttribute{
				MarkdownDescription: "Keys other tables hold into this one.",
				Computed:            true,
				NestedObject:        schema.NestedAttributeObject{Attributes: foreignKeyAttributes()},
			},
		},
	}
}

func foreignKeyAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name": schema.StringAttribute{
			MarkdownDescription: "Constraint name.",
			Computed:            true,
		},
		"source_schema": schema.StringAttribute{
			MarkdownDescription: "Schema holding the referencing table.",
			Computed:            true,
		},
		"source_table": schema.StringAttribute{
			MarkdownDescription: "Referencing table.",
			Computed:            true,
		},
		"source_columns": schema.ListAttribute{
			MarkdownDescription: "Referencing columns.",
			Computed:            true,
			ElementType:         types.StringType,
		},
		"target_schema": schema.StringAttribute{
			MarkdownDescription: "Schema holding the referenced table.",
			Computed:            true,
		},
		"target_table": schema.StringAttribute{
			MarkdownDescription: "Referenced table.",
			Computed:            true,
		},
		"target_columns": schema.ListAttribute{
			MarkdownDescription: "Referenced columns.",
			Computed:            true,
			ElementType:         types.StringType,
		},
	}
}

func (d *tableDetailDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	d.client = client
}

func (d *tableDetailDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	if d.client == nil {
		return
	}

	var state tableDetailModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	detail, err := pmonmcp.Call[pmonmcp.TableDetail](ctx, d.client, "get_table_detail", map[string]any{
		"datasource": state.Datasource.ValueString(),
		"schema":     state.Schema.ValueString(),
		"table":      state.Table.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Cannot read "+state.Schema.ValueString()+"."+state.Table.ValueString()+
				" on "+state.Datasource.ValueString(),
			err.Error(),
		)
		return
	}

	state.Columns = make([]detailColumnModel, 0, len(detail.Columns))
	for _, column := range detail.Columns {
		state.Columns = append(state.Columns, detailColumnModel{
			Name:          types.StringValue(column.Name),
			DataType:      types.StringValue(column.DataType),
			Ordinal:       types.Int64Value(column.Ordinal),
			Nullable:      types.BoolValue(column.Nullable),
			DefaultValue:  stringOrNull(column.DefaultValue),
			PartOfIndex:   types.BoolValue(column.PartOfIndex),
			AutoIncrement: types.BoolValue(column.AutoIncrement),
			Comment:       types.StringValue(column.Comment),
			Tags:          classificationTags(column.Classification),
		})
	}

	state.ForeignKeys = foreignKeyModels(detail.ForeignKeys)
	state.ReferencedBy = foreignKeyModels(detail.ReferencedBy)

	state.Engine = types.StringNull()
	state.EstimatedRows = types.Int64Null()
	state.OnDiskBytes = types.Int64Null()
	if detail.Metadata != nil {
		state.Engine = types.StringValue(detail.Metadata.Engine)
		state.EstimatedRows = types.Int64Value(detail.Metadata.EstimatedRows)
		state.OnDiskBytes = types.Int64Value(detail.Metadata.OnDiskBytes)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func foreignKeyModels(keys []pmonmcp.ForeignKey) []foreignKeyModel {
	out := make([]foreignKeyModel, 0, len(keys))
	for _, key := range keys {
		out = append(out, foreignKeyModel{
			Name:          types.StringValue(key.Name),
			SourceSchema:  types.StringValue(key.SourceSchema),
			SourceTable:   types.StringValue(key.SourceTable),
			SourceColumns: orEmpty(key.SourceColumns),
			TargetSchema:  types.StringValue(key.TargetSchema),
			TargetTable:   types.StringValue(key.TargetTable),
			TargetColumns: orEmpty(key.TargetColumns),
		})
	}
	return out
}

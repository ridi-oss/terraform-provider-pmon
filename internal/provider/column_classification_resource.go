package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

// maxClassificationBatch mirrors pmon's MAX_CLASSIFICATION_BATCH. Exceeding it is rejected
// server-side, so the plan says so instead.
const maxClassificationBatch = 500

var (
	_ resource.Resource                   = &columnClassificationResource{}
	_ resource.ResourceWithConfigure      = &columnClassificationResource{}
	_ resource.ResourceWithImportState    = &columnClassificationResource{}
	_ resource.ResourceWithValidateConfig = &columnClassificationResource{}
)

// NewColumnClassificationResource returns the column classification resource.
func NewColumnClassificationResource() resource.Resource { return &columnClassificationResource{} }

type columnClassificationResource struct {
	client *pmonmcp.Client
}

type classifiedColumnModel struct {
	Schema     types.String `tfsdk:"schema"`
	Table      types.String `tfsdk:"table"`
	Column     types.String `tfsdk:"column"`
	Tags       []string     `tfsdk:"tags"`
	MaskFnName types.String `tfsdk:"mask_fn_name"`
}

func (c classifiedColumnModel) key() string {
	return c.Schema.ValueString() + "." + c.Table.ValueString() + "." + c.Column.ValueString()
}

type columnClassificationResourceModel struct {
	Datasource types.String            `tfsdk:"datasource"`
	Columns    []classifiedColumnModel `tfsdk:"columns"`
}

func (r *columnClassificationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_column_classification"
}

func (r *columnClassificationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Tags on one datasource's columns.\n\n" +
			"This is the load-bearing half of every PII policy. A preset that reads everything `unless " +
			"resource in Tag::\"pii\"` protects nothing on a datasource whose columns carry no tags, so " +
			"the tags belong next to the policy that depends on them.\n\n" +
			"Applied as one all-or-nothing batch per datasource, so a rejected entry leaves nothing " +
			"half-tagged. Tags starting with `system:` are reserved for pmon's own classification.",
		Attributes: map[string]schema.Attribute{
			"datasource": schema.StringAttribute{
				MarkdownDescription: "Datasource whose columns these are. Changing it replaces the resource.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"columns": schema.SetNestedAttribute{
				MarkdownDescription: fmt.Sprintf("The classified columns, at most %d. A column removed "+
					"from this set has its classification cleared.", maxClassificationBatch),
				Required: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"schema": schema.StringAttribute{
							MarkdownDescription: "Schema holding the table. Optional when the datasource " +
								"declares a default schema.",
							Optional: true,
						},
						"table": schema.StringAttribute{
							MarkdownDescription: "Table holding the column.",
							Required:            true,
						},
						"column": schema.StringAttribute{
							MarkdownDescription: "Column to classify.",
							Required:            true,
						},
						"tags": schema.SetAttribute{
							MarkdownDescription: "Tags to attach. Cedar policies match on these, and a " +
								"column inherits its table's and datasource's tags on top. A set, because " +
								"order carries no meaning and comparing as a list would report a " +
								"reordering as a change.",
							Required:    true,
							ElementType: types.StringType,
						},
						"mask_fn_name": schema.StringAttribute{
							MarkdownDescription: "Masking function to apply when Cedar decides the column " +
								"is readable masked. Null leaves pmon's default.",
							Optional: true,
						},
					},
				},
			},
		},
	}
}

func (r *columnClassificationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	r.client = client
}

// ValidateConfig catches, during plan, what pmon would otherwise reject mid-apply: a batch over
// the limit, a reserved tag, or the same column named twice, which pmon treats as an error rather
// than last-one-wins because the result could not say which tag set decided masking.
func (r *columnClassificationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config columnClassificationResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if len(config.Columns) > maxClassificationBatch {
		resp.Diagnostics.AddAttributeError(
			path.Root("columns"),
			"Too many columns for one batch",
			fmt.Sprintf("pmon applies at most %d columns per call, and this has %d. Split it across "+
				"several resources.", maxClassificationBatch, len(config.Columns)),
		)
	}

	seen := make(map[string]struct{}, len(config.Columns))
	for _, column := range config.Columns {
		if column.Column.IsUnknown() || column.Table.IsUnknown() {
			continue
		}
		key := column.key()
		if _, duplicate := seen[key]; duplicate {
			resp.Diagnostics.AddAttributeError(
				path.Root("columns"),
				"Duplicate column",
				fmt.Sprintf("%s appears more than once. pmon rejects a batch naming one column twice, "+
					"because the result could not say which tag set decided its masking.", key),
			)
		}
		seen[key] = struct{}{}

		for _, tag := range column.Tags {
			if isReservedName(tag) {
				resp.Diagnostics.AddAttributeError(
					path.Root("columns"),
					"Reserved tag",
					fmt.Sprintf("%s carries the tag %q, and the %s prefix is reserved for pmon's own "+
						"classification.", key, tag, reservedPrefix),
				)
			}
		}
	}
}

func (r *columnClassificationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan columnClassificationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	if err := r.apply(ctx, plan); err != nil {
		resp.Diagnostics.AddError("Cannot classify columns of "+plan.Datasource.ValueString(), err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *columnClassificationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state columnClassificationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	live, err := pmonmcp.Call[[]pmonmcp.ColumnTag](ctx, r.client, "list_column_tags", map[string]any{
		"datasource": state.Datasource.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot read column tags of "+state.Datasource.ValueString(), err.Error())
		return
	}

	byKey := make(map[string]pmonmcp.ColumnTag, len(live))
	for _, column := range live {
		byKey[column.Schema+"."+column.Table+"."+column.Column] = column
	}

	// Only the columns this resource manages are reflected back. The datasource may carry others,
	// classified by hand or by another resource, and adopting them here would make every plan
	// propose deleting them.
	kept := make([]classifiedColumnModel, 0, len(state.Columns))
	for _, column := range state.Columns {
		found, ok := byKey[column.key()]
		if !ok {
			continue
		}
		column.Tags = sortedCopy(found.Tags)
		column.MaskFnName = stringOrNull(found.MaskFnName)
		kept = append(kept, column)
	}

	if len(kept) == 0 {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Columns = kept
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *columnClassificationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state columnClassificationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	if err := r.apply(ctx, plan); err != nil {
		resp.Diagnostics.AddError("Cannot classify columns of "+plan.Datasource.ValueString(), err.Error())
		return
	}

	// A column dropped from the set keeps its old tags unless they are cleared: the batch tool
	// writes what it is given and touches nothing else.
	wanted := make(map[string]struct{}, len(plan.Columns))
	for _, column := range plan.Columns {
		wanted[column.key()] = struct{}{}
	}
	for _, column := range state.Columns {
		if _, keep := wanted[column.key()]; keep {
			continue
		}
		if err := r.clear(ctx, plan.Datasource.ValueString(), column); err != nil && !isNotFound(err) {
			resp.Diagnostics.AddError("Cannot clear the classification of "+column.key(), err.Error())
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *columnClassificationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state columnClassificationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	for _, column := range state.Columns {
		if err := r.clear(ctx, state.Datasource.ValueString(), column); err != nil && !isNotFound(err) {
			resp.Diagnostics.AddError("Cannot clear the classification of "+column.key(), err.Error())
			return
		}
	}
}

// ImportState takes the datasource name and adopts every column pmon reports as classified on it.
func (r *columnClassificationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if strings.TrimSpace(req.ID) == "" {
		resp.Diagnostics.AddError("Malformed import id", "Expected a datasource name.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("datasource"), req.ID)...)

	if r.client == nil {
		return
	}
	live, err := pmonmcp.Call[[]pmonmcp.ColumnTag](ctx, r.client, "list_column_tags", map[string]any{
		"datasource": req.ID,
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot read column tags of "+req.ID, err.Error())
		return
	}

	columns := make([]classifiedColumnModel, 0, len(live))
	for _, column := range live {
		columns = append(columns, classifiedColumnModel{
			Schema:     types.StringValue(column.Schema),
			Table:      types.StringValue(column.Table),
			Column:     types.StringValue(column.Column),
			Tags:       sortedCopy(column.Tags),
			MaskFnName: stringOrNull(column.MaskFnName),
		})
	}
	sort.Slice(columns, func(i, j int) bool { return columns[i].key() < columns[j].key() })

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("columns"), columns)...)
}

func (r *columnClassificationResource) apply(ctx context.Context, plan columnClassificationResourceModel) error {
	if len(plan.Columns) == 0 {
		return nil
	}

	entries := make([]map[string]any, 0, len(plan.Columns))
	fingerprint := make([]string, 0, len(plan.Columns))
	for _, column := range plan.Columns {
		entry := map[string]any{
			"table":  column.Table.ValueString(),
			"column": column.Column.ValueString(),
			"tags":   column.Tags,
		}
		if !column.Schema.IsNull() {
			entry["schema"] = column.Schema.ValueString()
		}
		if !column.MaskFnName.IsNull() {
			entry["maskFnName"] = column.MaskFnName.ValueString()
		}
		entries = append(entries, entry)
		fingerprint = append(fingerprint, column.key()+"="+strings.Join(sortedCopy(column.Tags), "+")+"/"+column.MaskFnName.ValueString())
	}
	sort.Strings(fingerprint)

	return pmonmcp.Do(ctx, r.client, "set_column_classifications", map[string]any{
		"datasource":     plan.Datasource.ValueString(),
		"columns":        entries,
		"idempotencyKey": idempotencyKey(append([]string{"classification", plan.Datasource.ValueString()}, fingerprint...)...),
	})
}

func (r *columnClassificationResource) clear(ctx context.Context, datasource string, column classifiedColumnModel) error {
	args := map[string]any{
		"datasource":     datasource,
		"table":          column.Table.ValueString(),
		"column":         column.Column.ValueString(),
		"idempotencyKey": idempotencyKey("classification.clear", datasource, column.key()),
	}
	if !column.Schema.IsNull() {
		args["schema"] = column.Schema.ValueString()
	}
	return pmonmcp.Do(ctx, r.client, "clear_column_classification", args)
}

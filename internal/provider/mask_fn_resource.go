package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ resource.Resource                = &maskFnResource{}
	_ resource.ResourceWithConfigure   = &maskFnResource{}
	_ resource.ResourceWithImportState = &maskFnResource{}
)

// NewMaskFnResource returns the masking function resource.
func NewMaskFnResource() resource.Resource { return &maskFnResource{} }

type maskFnResource struct {
	client *pmonmcp.Client
}

type maskFnResourceModel struct {
	Name types.String `tfsdk:"name"`
	Kind types.String `tfsdk:"kind"`
	ID   types.Int64  `tfsdk:"id"`
}

func (r *maskFnResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mask_fn"
}

func (r *maskFnResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A masking function.\n\n" +
			"Cedar decides only whether a column is read unmasked, masked, or not at all. Which mask a " +
			"masked column actually gets is decided here and attached by a `pmon_column_classification`. " +
			"A policy that permits `result.read.masked` on a deployment with no mask functions defined " +
			"is worth checking against real output.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Function name, as a column classification refers to it.",
				Required:            true,
			},
			"kind": schema.StringAttribute{
				MarkdownDescription: "Masking strategy. pmon validates the value; changing it replaces " +
					"the function.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"id": schema.Int64Attribute{
				MarkdownDescription: "pmon's internal id.",
				Computed:            true,
			},
		},
	}
}

func (r *maskFnResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	r.client = client
}

func (r *maskFnResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan maskFnResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	name := plan.Name.ValueString()
	kind := plan.Kind.ValueString()

	if err := pmonmcp.Do(ctx, r.client, "create_mask_fn", map[string]any{
		"name":           name,
		"kind":           kind,
		"idempotencyKey": idempotencyKey("maskfn.create", name, kind),
	}); err != nil {
		resp.Diagnostics.AddError("Cannot create the pmon mask function "+name, err.Error())
		return
	}

	resp.Diagnostics.Append(r.refresh(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *maskFnResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state maskFnResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	fn, found, err := r.find(ctx, state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon mask functions", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(fn.Name)
	state.Kind = types.StringValue(fn.Kind)
	state.ID = types.Int64Value(fn.ID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *maskFnResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state maskFnResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	current := state.Name.ValueString()
	wanted := plan.Name.ValueString()

	args := map[string]any{
		"name":           current,
		"idempotencyKey": idempotencyKey("maskfn.update", current, wanted),
	}
	if wanted != current {
		args["newName"] = wanted
	}

	if err := pmonmcp.Do(ctx, r.client, "update_mask_fn", args); err != nil {
		resp.Diagnostics.AddError("Cannot update the pmon mask function "+current, updateErrorDetail(err))
		return
	}

	resp.Diagnostics.Append(r.refresh(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *maskFnResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state maskFnResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	name := state.Name.ValueString()
	err := pmonmcp.Do(ctx, r.client, "delete_mask_fn", map[string]any{
		"name":           name,
		"idempotencyKey": idempotencyKey("maskfn.delete", name),
	})
	if err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the pmon mask function "+name, updateErrorDetail(err))
	}
}

func (r *maskFnResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func (r *maskFnResource) find(ctx context.Context, name string) (pmonmcp.MaskFn, bool, error) {
	fns, err := pmonmcp.Call[[]pmonmcp.MaskFn](ctx, r.client, "list_mask_fns", map[string]any{})
	if err != nil {
		return pmonmcp.MaskFn{}, false, err
	}
	for _, fn := range fns {
		if fn.Name == name {
			return fn, true, nil
		}
	}
	return pmonmcp.MaskFn{}, false, nil
}

func (r *maskFnResource) refresh(ctx context.Context, model *maskFnResourceModel) (diags diag.Diagnostics) {
	fn, found, err := r.find(ctx, model.Name.ValueString())
	if err != nil {
		diags.AddError("Cannot list pmon mask functions", err.Error())
		return diags
	}
	if !found {
		diags.AddError(
			"Mask function missing after write",
			"pmon accepted the write but does not list a mask function named "+model.Name.ValueString()+".",
		)
		return diags
	}
	model.Kind = types.StringValue(fn.Kind)
	model.ID = types.Int64Value(fn.ID)
	return diags
}

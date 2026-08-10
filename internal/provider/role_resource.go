package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ resource.Resource                   = &roleResource{}
	_ resource.ResourceWithConfigure      = &roleResource{}
	_ resource.ResourceWithImportState    = &roleResource{}
	_ resource.ResourceWithValidateConfig = &roleResource{}
)

// NewRoleResource returns the access-control role resource.
func NewRoleResource() resource.Resource { return &roleResource{} }

type roleResource struct {
	client *pmonmcp.Client
}

type roleResourceModel struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ID          types.Int64  `tfsdk:"id"`
}

func (r *roleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *roleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An access-control role.\n\n" +
			"A role is only a name Cedar can match on: it grants nothing by itself. Policies decide what " +
			"holding it means, and `pmon_group_roles` or `pmon_role_assignment` decide who holds it.\n\n" +
			"Shipped `system:` roles are immutable and cannot be managed here.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Role name, as Cedar policies refer to it: `Role::\"<name>\"`. " +
					"Renaming updates the role in place, so policies naming the old value stop matching.",
				Required: true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What holding the role means. Shown in pmon's console.",
				Optional:            true,
			},
			"id": schema.Int64Attribute{
				MarkdownDescription: "pmon's internal id.",
				Computed:            true,
			},
		},
	}
}

func (r *roleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	r.client = client
}

func (r *roleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config roleResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.Name.IsUnknown() || config.Name.IsNull() {
		return
	}

	if isReservedName(config.Name.ValueString()) {
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Reserved role name",
			fmt.Sprintf("%q is in pmon's %s namespace, which holds shipped roles that cannot be "+
				"modified. Choose a name outside it.", config.Name.ValueString(), reservedPrefix),
		)
	}
}

func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	name := plan.Name.ValueString()
	args := map[string]any{
		"name":           name,
		"idempotencyKey": idempotencyKey("role.create", name),
	}
	if !plan.Description.IsNull() {
		args["description"] = plan.Description.ValueString()
	}

	created, err := pmonmcp.Call[pmonmcp.Role](ctx, r.client, "create_role", args)
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the pmon role "+name, err.Error())
		return
	}

	applyRole(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	name := state.Name.ValueString()
	roles, err := pmonmcp.Call[[]pmonmcp.Role](ctx, r.client, "list_roles", map[string]any{})
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon roles", err.Error())
		return
	}

	for _, role := range roles {
		if role.Name == name {
			applyRole(&state, role)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *roleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state roleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	current := state.Name.ValueString()
	wanted := plan.Name.ValueString()

	args := map[string]any{
		"name":           current,
		"idempotencyKey": idempotencyKey("role.update", current, wanted, plan.Description.ValueString()),
	}
	if wanted != current {
		args["newName"] = wanted
	}
	if !plan.Description.IsNull() {
		args["description"] = plan.Description.ValueString()
	}

	updated, err := pmonmcp.Call[pmonmcp.Role](ctx, r.client, "update_role", args)
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the pmon role "+current, updateErrorDetail(err))
		return
	}

	applyRole(&plan, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	name := state.Name.ValueString()
	err := pmonmcp.Do(ctx, r.client, "delete_role", map[string]any{
		"name":           name,
		"idempotencyKey": idempotencyKey("role.delete", name),
	})
	if err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the pmon role "+name, updateErrorDetail(err))
	}
}

func (r *roleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func applyRole(model *roleResourceModel, role pmonmcp.Role) {
	model.Name = types.StringValue(role.Name)
	model.ID = types.Int64Value(role.ID)
	// pmon echoes a description it does not have as absent; keeping the planned null avoids an
	// "inconsistent result after apply" when the caller set none.
	if role.Description != nil {
		model.Description = types.StringValue(*role.Description)
	}
}

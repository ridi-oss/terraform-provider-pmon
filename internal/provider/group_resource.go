package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ resource.Resource                   = &groupResource{}
	_ resource.ResourceWithConfigure      = &groupResource{}
	_ resource.ResourceWithImportState    = &groupResource{}
	_ resource.ResourceWithValidateConfig = &groupResource{}
)

// NewGroupResource returns the local identity group resource.
func NewGroupResource() resource.Resource { return &groupResource{} }

type groupResource struct {
	client *pmonmcp.Client
}

type groupResourceModel struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ID          types.Int64  `tfsdk:"id"`
	Source      types.String `tfsdk:"source"`
	MemberCount types.Int64  `tfsdk:"member_count"`
}

func (r *groupResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

func (r *groupResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A local identity group.\n\n" +
			"This manages the group object only. Its roles are `pmon_group_roles` and its members are " +
			"`pmon_group_member`, kept separate because a group's members frequently come from the IdP " +
			"even when its roles are managed here.\n\n" +
			"Groups provisioned from the IdP group claim have source `OIDC` and shipped groups have " +
			"source `SYSTEM`; neither can be created here, though `pmon_group_roles` can still set the " +
			"roles of an OIDC group.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Group name.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What the group is for.",
				Optional:            true,
			},
			"id": schema.Int64Attribute{
				MarkdownDescription: "pmon's internal id.",
				Computed:            true,
			},
			"source": schema.StringAttribute{
				MarkdownDescription: "`LOCAL` for a group created here, `OIDC` for one provisioned from " +
					"the IdP group claim, `SYSTEM` for a shipped group.",
				Computed: true,
			},
			"member_count": schema.Int64Attribute{
				MarkdownDescription: "How many members the group has. pmon reports only the count here; " +
					"who they are is visible through the user listing.",
				Computed: true,
			},
		},
	}
}

func (r *groupResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	r.client = client
}

func (r *groupResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config groupResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.Name.IsUnknown() || config.Name.IsNull() {
		return
	}

	if isReservedName(config.Name.ValueString()) {
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Reserved group name",
			fmt.Sprintf("%q is in pmon's %s namespace, which holds shipped groups that cannot be "+
				"modified. Choose a name outside it.", config.Name.ValueString(), reservedPrefix),
		)
	}
}

func (r *groupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	name := plan.Name.ValueString()
	args := map[string]any{
		"name":           name,
		"idempotencyKey": idempotencyKey("group.create", name),
	}
	if !plan.Description.IsNull() {
		args["description"] = plan.Description.ValueString()
	}

	if err := pmonmcp.Do(ctx, r.client, "create_group", args); err != nil {
		resp.Diagnostics.AddError("Cannot create the pmon group "+name, err.Error())
		return
	}
	r.client.Listings().Invalidate()

	resp.Diagnostics.Append(r.refresh(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	group, found, err := r.client.Listings().Group(ctx, state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon groups", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	applyGroup(&state, group)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state groupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	current := state.Name.ValueString()
	wanted := plan.Name.ValueString()

	args := map[string]any{
		"name":           current,
		"idempotencyKey": idempotencyKey("group.update", current, wanted, plan.Description.ValueString()),
	}
	if wanted != current {
		args["newName"] = wanted
	}
	if !plan.Description.IsNull() {
		args["description"] = plan.Description.ValueString()
	}

	if err := pmonmcp.Do(ctx, r.client, "update_group", args); err != nil {
		resp.Diagnostics.AddError("Cannot update the pmon group "+current, updateErrorDetail(err))
		return
	}
	r.client.Listings().Invalidate()

	resp.Diagnostics.Append(r.refresh(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	name := state.Name.ValueString()
	err := pmonmcp.Do(ctx, r.client, "delete_group", map[string]any{
		"name":           name,
		"idempotencyKey": idempotencyKey("group.delete", name),
	})
	if err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the pmon group "+name, updateErrorDetail(err))
		return
	}
	r.client.Listings().Invalidate()
}

func (r *groupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// refresh reads back what pmon actually stored. The write tools answer without a body, and the
// computed attributes have to come from somewhere.
func (r *groupResource) refresh(ctx context.Context, model *groupResourceModel) (diags diag.Diagnostics) {
	group, found, err := r.client.Listings().Group(ctx, model.Name.ValueString())
	if err != nil {
		diags.AddError("Cannot list pmon groups", err.Error())
		return diags
	}
	if !found {
		diags.AddError(
			"Group missing after write",
			fmt.Sprintf("pmon accepted the write but does not list a group named %q.", model.Name.ValueString()),
		)
		return diags
	}
	applyGroup(model, group)
	return diags
}

func applyGroup(model *groupResourceModel, group pmonmcp.Group) {
	model.Name = types.StringValue(group.Name)
	model.ID = types.Int64Value(group.ID)
	model.Source = types.StringValue(group.Source)
	model.MemberCount = types.Int64Value(group.MemberCount)
	if group.Description != nil {
		model.Description = types.StringValue(*group.Description)
	}
}

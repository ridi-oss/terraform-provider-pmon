package provider

import (
	"context"
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

var (
	_ resource.Resource                = &groupRolesResource{}
	_ resource.ResourceWithConfigure   = &groupRolesResource{}
	_ resource.ResourceWithImportState = &groupRolesResource{}
)

// NewGroupRolesResource returns the resource binding roles to a group.
func NewGroupRolesResource() resource.Resource { return &groupRolesResource{} }

type groupRolesResource struct {
	client *pmonmcp.Client
}

type groupRolesResourceModel struct {
	GroupName types.String `tfsdk:"group_name"`
	RoleNames []string     `tfsdk:"role_names"`
}

func (r *groupRolesResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_roles"
}

func (r *groupRolesResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The complete set of roles bound to a group.\n\n" +
			"This is authoritative: pmon's tool replaces the group's roles outright, so any role not " +
			"listed here is removed. Destroying the resource leaves the group with no roles rather than " +
			"deleting the group.\n\n" +
			"This works on an `OIDC` group too, which is the usual way an IdP-provisioned team gets its " +
			"entitlements without the membership being managed here.",
		Attributes: map[string]schema.Attribute{
			"group_name": schema.StringAttribute{
				MarkdownDescription: "Group whose roles these are. Changing it moves the binding, so the " +
					"resource is replaced.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"role_names": schema.SetAttribute{
				MarkdownDescription: "Every role the group should hold. A set, because order carries no " +
					"meaning here and comparing as a list would report a reordering as a change.",
				Required:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func (r *groupRolesResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	r.client = client
}

func (r *groupRolesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupRolesResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	if err := r.set(ctx, plan.GroupName.ValueString(), plan.RoleNames); err != nil {
		resp.Diagnostics.AddError("Cannot set the roles of the pmon group "+plan.GroupName.ValueString(), updateErrorDetail(err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupRolesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupRolesResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	group, found, err := r.client.Listings().Group(ctx, state.GroupName.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon groups", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	// pmon returns roles in its own order. The attribute is a set, so that order is not compared;
	// sorting only keeps the state file stable enough to diff by eye.
	state.RoleNames = sortedCopy(group.RoleNames())
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *groupRolesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan groupRolesResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	if err := r.set(ctx, plan.GroupName.ValueString(), plan.RoleNames); err != nil {
		resp.Diagnostics.AddError("Cannot set the roles of the pmon group "+plan.GroupName.ValueString(), updateErrorDetail(err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete clears the group's roles. The group itself is not this resource's to remove.
func (r *groupRolesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupRolesResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	err := r.set(ctx, state.GroupName.ValueString(), []string{})
	if err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Cannot clear the roles of the pmon group "+state.GroupName.ValueString(), updateErrorDetail(err))
	}
}

func (r *groupRolesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("group_name"), req, resp)
}

func (r *groupRolesResource) set(ctx context.Context, group string, roles []string) error {
	if roles == nil {
		roles = []string{}
	}
	err := pmonmcp.Do(ctx, r.client, "set_group_roles", map[string]any{
		"groupName":      group,
		"roleNames":      roles,
		"idempotencyKey": idempotencyKey("group.roles", group, strings.Join(sortedCopy(roles), ",")),
	})
	if err != nil {
		return err
	}
	r.client.Listings().Invalidate()
	return nil
}

// sortedCopy sorts without disturbing the caller's slice, which Terraform still holds.
func sortedCopy(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	sort.Strings(out)
	return out
}

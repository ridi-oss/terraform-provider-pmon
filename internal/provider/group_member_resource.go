package provider

import (
	"context"
	"fmt"
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
	_ resource.Resource                = &groupMemberResource{}
	_ resource.ResourceWithConfigure   = &groupMemberResource{}
	_ resource.ResourceWithImportState = &groupMemberResource{}
)

// NewGroupMemberResource returns the resource placing one principal in one group.
func NewGroupMemberResource() resource.Resource { return &groupMemberResource{} }

type groupMemberResource struct {
	client *pmonmcp.Client
}

type groupMemberResourceModel struct {
	GroupName types.String `tfsdk:"group_name"`
	Principal types.String `tfsdk:"principal"`
}

func (r *groupMemberResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_member"
}

func (r *groupMemberResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One principal's membership of one group.\n\n" +
			"Non-authoritative: it adds and removes exactly this membership and leaves the group's other " +
			"members alone, which is what makes it safe to use on a group the IdP also populates.\n\n" +
			"Membership of an `OIDC` group is reconciled from the IdP group claim on login, so managing " +
			"it here will fight the IdP. Manage `LOCAL` groups here and let the IdP own its own.",
		Attributes: map[string]schema.Attribute{
			"group_name": schema.StringAttribute{
				MarkdownDescription: "Group to place the principal in.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"principal": schema.StringAttribute{
				MarkdownDescription: "Principal to add, as pmon knows it, usually an email address.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *groupMemberResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	r.client = client
}

func (r *groupMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	group := plan.GroupName.ValueString()
	principal := plan.Principal.ValueString()

	err := pmonmcp.Do(ctx, r.client, "add_group_member", map[string]any{
		"groupName":      group,
		"principal":      principal,
		"idempotencyKey": idempotencyKey("group.member.add", group, principal),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot add "+principal+" to the pmon group "+group, err.Error())
		return
	}
	r.client.Listings().Invalidate()

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read goes through the user listing: a group listing reports only how many members it has, so
// there is no way to ask a group who its members are.
func (r *groupMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	user, found, err := r.client.Listings().User(ctx, state.Principal.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon users", err.Error())
		return
	}
	if !found || !user.InGroup(state.GroupName.ValueString()) {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update cannot happen: both attributes force replacement.
func (r *groupMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Group membership cannot be updated in place",
		"Both attributes replace the resource, so this is a bug in the provider.",
	)
}

func (r *groupMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	group := state.GroupName.ValueString()
	principal := state.Principal.ValueString()

	err := pmonmcp.Do(ctx, r.client, "remove_group_member", map[string]any{
		"groupName":      group,
		"principal":      principal,
		"idempotencyKey": idempotencyKey("group.member.remove", group, principal),
	})
	if err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Cannot remove "+principal+" from the pmon group "+group, err.Error())
		return
	}
	r.client.Listings().Invalidate()
}

func (r *groupMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	group, principal, found := strings.Cut(req.ID, importSeparator)
	if !found || group == "" || principal == "" {
		resp.Diagnostics.AddError(
			"Malformed import id",
			fmt.Sprintf("Expected %q, got %q.", "<group name>"+importSeparator+"<principal>", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_name"), group)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal"), principal)...)
}

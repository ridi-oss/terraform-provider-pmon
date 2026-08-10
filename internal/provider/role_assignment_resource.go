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

// importSeparator joins the two halves of a composite import id. A comma cannot appear in a group
// name or a principal, so it splits unambiguously.
const importSeparator = ","

var (
	_ resource.Resource                = &roleAssignmentResource{}
	_ resource.ResourceWithConfigure   = &roleAssignmentResource{}
	_ resource.ResourceWithImportState = &roleAssignmentResource{}
)

// NewRoleAssignmentResource returns the resource granting one role to one principal directly.
func NewRoleAssignmentResource() resource.Resource { return &roleAssignmentResource{} }

type roleAssignmentResource struct {
	client *pmonmcp.Client
}

type roleAssignmentResourceModel struct {
	Principal types.String `tfsdk:"principal"`
	RoleName  types.String `tfsdk:"role_name"`
}

// assignment is one row of list_role_assignments. The field names are inferred from the
// assign_role tool's arguments; this deployment has no direct assignments to confirm them
// against, so Read tolerates either shape rather than trusting one.
type assignment struct {
	Principal string  `json:"principal"`
	RoleName  string  `json:"roleName"`
	Role      *string `json:"role"`
}

func (a assignment) role() string {
	if a.RoleName != "" {
		return a.RoleName
	}
	if a.Role != nil {
		return *a.Role
	}
	return ""
}

func (r *roleAssignmentResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role_assignment"
}

func (r *roleAssignmentResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A role granted straight to a principal, bypassing groups.\n\n" +
			"Prefer `pmon_group_roles`: a direct assignment is invisible in the group structure and is " +
			"easy to lose track of when someone changes team. Reach for this only when an entitlement " +
			"genuinely belongs to one person and no group describes it.",
		Attributes: map[string]schema.Attribute{
			"principal": schema.StringAttribute{
				MarkdownDescription: "Principal to grant the role to.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"role_name": schema.StringAttribute{
				MarkdownDescription: "Role to grant.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *roleAssignmentResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	r.client = client
}

func (r *roleAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleAssignmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	principal := plan.Principal.ValueString()
	role := plan.RoleName.ValueString()

	err := pmonmcp.Do(ctx, r.client, "assign_role", map[string]any{
		"principal":      principal,
		"roleName":       role,
		"idempotencyKey": idempotencyKey("role.assign", principal, role),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot assign "+role+" to "+principal, err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *roleAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	principal := state.Principal.ValueString()
	assignments, err := pmonmcp.Call[[]assignment](ctx, r.client, "list_role_assignments", map[string]any{
		"principal": principal,
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon role assignments", err.Error())
		return
	}

	for _, a := range assignments {
		if a.role() == state.RoleName.ValueString() {
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

// Update cannot happen: both attributes force replacement.
func (r *roleAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Role assignments cannot be updated in place",
		"Both attributes replace the resource, so this is a bug in the provider.",
	)
}

func (r *roleAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleAssignmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	principal := state.Principal.ValueString()
	role := state.RoleName.ValueString()

	err := pmonmcp.Do(ctx, r.client, "unassign_role", map[string]any{
		"principal":      principal,
		"roleName":       role,
		"idempotencyKey": idempotencyKey("role.unassign", principal, role),
	})
	if err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Cannot unassign "+role+" from "+principal, err.Error())
	}
}

func (r *roleAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	principal, role, found := strings.Cut(req.ID, importSeparator)
	if !found || principal == "" || role == "" {
		resp.Diagnostics.AddError(
			"Malformed import id",
			fmt.Sprintf("Expected %q, got %q.", "<principal>"+importSeparator+"<role name>", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal"), principal)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role_name"), role)...)
}

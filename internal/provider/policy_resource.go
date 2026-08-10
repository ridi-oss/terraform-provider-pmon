package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ resource.Resource                = &policyResource{}
	_ resource.ResourceWithConfigure   = &policyResource{}
	_ resource.ResourceWithImportState = &policyResource{}
	_ resource.ResourceWithModifyPlan  = &policyResource{}
)

// NewPolicyResource returns the Cedar policy resource.
func NewPolicyResource() resource.Resource { return &policyResource{} }

type policyResource struct {
	client *pmonmcp.Client
}

type policyResourceModel struct {
	Name      types.String `tfsdk:"name"`
	CedarSrc  types.String `tfsdk:"cedar_src"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	ID        types.Int64  `tfsdk:"id"`
	Origin    types.String `tfsdk:"origin"`
	UpdatedBy types.String `tfsdk:"updated_by"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func (r *policyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_policy"
}

func (r *policyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Cedar policy.\n\n" +
			"Cedar evaluates every policy on every decision, and a `forbid` always beats a `permit`, so " +
			"a policy added here layers over the shipped `system:` presets rather than replacing them. " +
			"The source is validated during `terraform plan`, not at apply.\n\n" +
			"Shipped `system:` policies are immutable and cannot be managed here. Read them with the " +
			"`pmon_policy` data source, or toggle nothing and use `enabled` on your own policies.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Policy name. Renaming replaces the policy.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"cedar_src": schema.StringAttribute{
				MarkdownDescription: "Cedar source. Validated against the schema from the " +
					"`pmon_policy_schema` data source during plan.",
				Required: true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the policy takes part in decisions. Setting this to `false` " +
					"is the quickest way to withdraw a policy without deleting it. Defaults to `true`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"id": schema.Int64Attribute{
				MarkdownDescription: "pmon's internal id.",
				Computed:            true,
			},
			"origin": schema.StringAttribute{
				MarkdownDescription: "Always `USER` for a managed policy.",
				Computed:            true,
			},
			"updated_by": schema.StringAttribute{
				MarkdownDescription: "Principal that last wrote the policy. This is the person who ran " +
					"terraform, since the provider authenticates as a human.",
				Computed: true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "When the policy was last written.",
				Computed:            true,
			},
		},
	}
}

func (r *policyResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	r.client = client
}

// ModifyPlan runs the policy through pmon's validator before anything is written. validate_policy
// writes nothing, so this is safe during plan and turns a Cedar syntax error into a plan failure
// pointing at the attribute rather than a failed apply.
func (r *policyResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.client == nil {
		return
	}

	var plan policyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.Name.IsUnknown() || plan.CedarSrc.IsUnknown() {
		return
	}

	if isReservedName(plan.Name.ValueString()) {
		resp.Diagnostics.AddAttributeError(
			path.Root("name"),
			"Reserved policy name",
			fmt.Sprintf("%q is in pmon's %s namespace, which holds shipped policies that cannot be "+
				"modified. Choose a name outside it.", plan.Name.ValueString(), reservedPrefix),
		)
		return
	}

	result, err := pmonmcp.Call[pmonmcp.ValidationResult](ctx, r.client, "validate_policy", map[string]any{
		"cedarSrc": plan.CedarSrc.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddAttributeWarning(
			path.Root("cedar_src"),
			"Could not validate the Cedar source",
			"The policy will still be validated when it is applied. "+err.Error(),
		)
		return
	}
	if !result.Valid {
		resp.Diagnostics.AddAttributeError(
			path.Root("cedar_src"),
			"Invalid Cedar policy",
			strings.Join(result.Errors, "\n"),
		)
	}
}

func (r *policyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan policyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	name := plan.Name.ValueString()
	created, err := pmonmcp.Call[pmonmcp.Policy](ctx, r.client, "create_policy", map[string]any{
		"name":           name,
		"cedarSrc":       plan.CedarSrc.ValueString(),
		"enabled":        plan.Enabled.ValueBool(),
		"idempotencyKey": idempotencyKey("policy.create", name, plan.CedarSrc.ValueString()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the pmon policy "+name, err.Error())
		return
	}

	applyPolicy(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *policyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state policyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	name := state.Name.ValueString()
	policy, err := pmonmcp.Call[pmonmcp.Policy](ctx, r.client, "get_policy", map[string]any{"name": name})
	if err != nil {
		// A policy deleted outside terraform is drift to reconcile, not an error to raise.
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Cannot read the pmon policy "+name, err.Error())
		return
	}

	applyPolicy(&state, policy)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *policyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan policyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	name := plan.Name.ValueString()
	source := plan.CedarSrc.ValueString()
	enabled := plan.Enabled.ValueBool()

	updated, err := pmonmcp.Call[pmonmcp.Policy](ctx, r.client, "update_policy", map[string]any{
		"name":           name,
		"cedarSrc":       source,
		"enabled":        enabled,
		"idempotencyKey": idempotencyKey("policy.update", name, source, fmt.Sprint(enabled)),
	})
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the pmon policy "+name, updateErrorDetail(err))
		return
	}

	applyPolicy(&plan, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *policyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state policyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}

	name := state.Name.ValueString()
	err := pmonmcp.Do(ctx, r.client, "delete_policy", map[string]any{
		"name":           name,
		"idempotencyKey": idempotencyKey("policy.delete", name),
	})
	if err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the pmon policy "+name, updateErrorDetail(err))
	}
}

func (r *policyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func applyPolicy(model *policyResourceModel, policy pmonmcp.Policy) {
	model.Name = types.StringValue(policy.Name)
	model.CedarSrc = types.StringValue(policy.CedarSrc)
	model.Enabled = types.BoolValue(policy.Enabled)
	model.ID = types.Int64Value(policy.ID)
	model.Origin = types.StringValue(policy.Origin)
	model.UpdatedBy = stringOrNull(policy.UpdatedBy)
	model.UpdatedAt = stringOrNull(policy.UpdatedAt)
}

// updateErrorDetail explains pmon's immutability refusal, which otherwise reads as an opaque
// error code.
func updateErrorDetail(err error) string {
	if pmonmcp.IsSystemImmutable(err) {
		return err.Error() + "\n\nThis is a shipped SYSTEM row. pmon does not allow it to be " +
			"modified or deleted; remove it from the configuration and read it with a data source instead."
	}
	return err.Error()
}

// isNotFound recognises pmon reporting an absent row. There is no dedicated code, so this matches
// the message: a false negative costs a confusing error, never a wrong write.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not found") ||
		strings.Contains(message, "not_found") ||
		strings.Contains(message, "no such")
}

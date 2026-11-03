package provider

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

var (
	_ resource.Resource                   = &workloadBindingResource{}
	_ resource.ResourceWithConfigure      = &workloadBindingResource{}
	_ resource.ResourceWithImportState    = &workloadBindingResource{}
	_ resource.ResourceWithValidateConfig = &workloadBindingResource{}
)

var (
	bindingNamePattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	servicePrincipalPattern = regexp.MustCompile(`^svc:[a-z0-9][a-z0-9._-]{0,62}$`)
	bindingPurposes         = []string{"mcp", "wire"}
	bindingMCPScopes        = []string{"mcp:approvals:write", "mcp:datasources:write", "mcp:identity:write", "mcp:policies:write", "mcp:query", "mcp:read", "mcp:tokens"}
	conditionValuesType     = types.SetType{ElemType: types.StringType}
)

const (
	defaultBindingTTL = 900
	minBindingTTL     = 60
	maxBindingTTL     = 3600
)

func NewWorkloadBindingResource() resource.Resource { return &workloadBindingResource{} }

type workloadBindingResource struct {
	client *pmonmcp.Client
}

type workloadBindingModel struct {
	ID          types.Int64  `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Issuer      types.String `tfsdk:"issuer"`
	Subject     types.String `tfsdk:"subject"`
	Conditions  types.Map    `tfsdk:"conditions"`
	Principal   types.String `tfsdk:"principal"`
	Purposes    types.Set    `tfsdk:"purposes"`
	MCPScopes   types.Set    `tfsdk:"mcp_scopes"`
	TTLSeconds  types.Int64  `tfsdk:"ttl_seconds"`
	Enabled     types.Bool   `tfsdk:"enabled"`
}

func (r *workloadBindingResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workload_binding"
}

func (r *workloadBindingResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Maps a workload's OIDC token to a pmon service principal.\n\n" +
			"A service with no human behind it -- an AWS role (through IAM outbound identity federation), a " +
			"GitHub Actions job, an HCP Terraform run -- exchanges the short-lived token its platform issues at " +
			"pmon's `/api/workload/exchange`. pmon verifies it against a trusted issuer and mints a credential " +
			"for the one enabled binding whose `subject` and every `conditions` entry match: a wire token for " +
			"SQL through the proxy, an MCP access token, or either.\n\n" +
			"The principal holds roles like anyone else; grant them with `pmon_role_assignment`. Disabling, " +
			"deleting, or narrowing a binding revokes every token it minted.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Binding name: lowercase letters, digits, and hyphens. Changing it replaces the binding.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What the workload is. Shown in pmon's console.",
				Optional:            true,
			},
			"issuer": schema.StringAttribute{
				MarkdownDescription: "Name of a trusted issuer configured on pmon (`PM_CI_OIDC_ISSUERS`), for example `aws-prod`, `github`, or `hcp-terraform`.",
				Required:            true,
			},
			"subject": schema.StringAttribute{
				MarkdownDescription: "The token's exact `sub`, or a prefix ending in `*`. An AWS token's `sub` is the IAM role ARN " +
					"with its path; a GitHub Actions token's is `repo:<owner>/<repo>:environment:<env>`; an HCP Terraform " +
					"token's is `organization:<org>:project:<project>:workspace:<workspace>:run_phase:<plan|apply>`.",
				Required: true,
			},
			"conditions": schema.MapAttribute{
				ElementType: conditionValuesType,
				MarkdownDescription: "Claims that must also hold, as claim name to accepted values (each exact, or a prefix ending in `*`). " +
					"Nested claims join their keys with a dot and AWS's own claims sit at the top level, for example " +
					"`aws_account`, `principal_tags.kubernetes-namespace`, `repository`, or `terraform_run_phase`.",
				Optional: true,
			},
			"principal": schema.StringAttribute{
				MarkdownDescription: "Service principal the token acts as, `svc:<name>`. Defaults to `svc:` followed by the binding name.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{principalFromName{}},
			},
			"purposes": schema.SetAttribute{
				ElementType:         types.StringType,
				MarkdownDescription: "What an exchange may mint: `wire` (a SQL wire token) and `mcp` (an MCP access token).",
				Required:            true,
			},
			"mcp_scopes": schema.SetAttribute{
				ElementType: types.StringType,
				MarkdownDescription: "The most an `mcp` exchange may carry. Required when `purposes` contains `mcp`, empty otherwise. " +
					"Valid scopes are `" + strings.Join(bindingMCPScopes, "`, `") + "`.",
				Optional: true,
				Computed: true,
				Default:  setdefault.StaticValue(types.SetValueMust(types.StringType, nil)),
			},
			"ttl_seconds": schema.Int64Attribute{
				MarkdownDescription: "Lifetime of a minted token, 60 to 3600 seconds. An MCP token is also capped by pmon's MCP access token lifetime.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(defaultBindingTTL),
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the binding matches tokens. Disabling it revokes what it minted.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"id": schema.Int64Attribute{
				MarkdownDescription: "pmon's internal id.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *workloadBindingResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	client, diags := clientFromProviderData(req.ProviderData)
	resp.Diagnostics.Append(diags...)
	r.client = client
}

func (r *workloadBindingResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config workloadBindingModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateWorkloadBinding(ctx, config)...)
}

func validateWorkloadBinding(ctx context.Context, config workloadBindingModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if known(config.Name) && !bindingNamePattern.MatchString(config.Name.ValueString()) {
		diags.AddAttributeError(path.Root("name"), "Invalid workload binding name",
			"Use 1 to 63 lowercase letters, digits, and hyphens, starting with a letter or digit.")
	}
	if known(config.Subject) && strings.TrimSpace(config.Subject.ValueString()) == "*" {
		diags.AddAttributeError(path.Root("subject"), "Subject matches every token",
			"A lone * would bind every token the issuer signs. Name the subject, or a prefix of it.")
	}
	if known(config.Principal) && !servicePrincipalPattern.MatchString(config.Principal.ValueString()) {
		diags.AddAttributeError(path.Root("principal"), "Invalid service principal",
			"A workload binding's principal is svc:<name>, with lowercase letters, digits, dots, underscores, and hyphens.")
	}
	if !config.TTLSeconds.IsNull() && !config.TTLSeconds.IsUnknown() {
		if ttl := config.TTLSeconds.ValueInt64(); ttl < minBindingTTL || ttl > maxBindingTTL {
			diags.AddAttributeError(path.Root("ttl_seconds"), "TTL out of range",
				fmt.Sprintf("ttl_seconds must be between %d and %d.", minBindingTTL, maxBindingTTL))
		}
	}
	purposes, purposesKnown := setStrings(ctx, config.Purposes, &diags)
	for _, p := range purposes {
		if !slices.Contains(bindingPurposes, p) {
			diags.AddAttributeError(path.Root("purposes"), "Unknown purpose", fmt.Sprintf("%q is not wire or mcp.", p))
		}
	}
	if purposesKnown && len(purposes) == 0 {
		diags.AddAttributeError(path.Root("purposes"), "No purpose", "Name at least one of wire and mcp.")
	}
	scopes, scopesKnown := setStrings(ctx, config.MCPScopes, &diags)
	for _, s := range scopes {
		if !slices.Contains(bindingMCPScopes, s) {
			diags.AddAttributeError(path.Root("mcp_scopes"), "Unknown pmon scope",
				fmt.Sprintf("%q is not a scope pmon defines. Valid scopes are %s.", s, strings.Join(bindingMCPScopes, ", ")))
		}
	}
	if purposesKnown && scopesKnown && slices.Contains(purposes, "mcp") != (len(scopes) > 0) {
		diags.AddAttributeError(path.Root("mcp_scopes"), "mcp_scopes and purposes disagree",
			"mcp_scopes is required when purposes contains mcp, and must be empty otherwise.")
	}
	if !config.Conditions.IsNull() && !config.Conditions.IsUnknown() {
		var conditions map[string][]string
		diags.Append(config.Conditions.ElementsAs(ctx, &conditions, false)...)
		for claim, values := range conditions {
			if strings.TrimSpace(claim) == "" || len(values) == 0 || slices.ContainsFunc(values, func(v string) bool { return v == "" || v == "*" }) {
				diags.AddAttributeError(path.Root("conditions"), "Invalid condition",
					fmt.Sprintf("Condition %q needs a claim name and at least one value, none of them empty or a lone *.", claim))
			}
		}
	}
	return diags
}

func known(v types.String) bool { return !v.IsNull() && !v.IsUnknown() }

func setStrings(ctx context.Context, set types.Set, diags *diag.Diagnostics) ([]string, bool) {
	if set.IsNull() {
		return nil, true
	}
	if set.IsUnknown() || slices.ContainsFunc(set.Elements(), func(v attr.Value) bool { return v.IsUnknown() }) {
		return nil, false
	}
	var out []string
	diags.Append(set.ElementsAs(ctx, &out, false)...)
	return out, true
}

func bindingArgs(ctx context.Context, plan workloadBindingModel, diags *diag.Diagnostics) map[string]any {
	purposes, _ := setStrings(ctx, plan.Purposes, diags)
	scopes, _ := setStrings(ctx, plan.MCPScopes, diags)
	args := map[string]any{
		"name":       plan.Name.ValueString(),
		"issuer":     plan.Issuer.ValueString(),
		"subject":    plan.Subject.ValueString(),
		"purposes":   sortedStrings(purposes),
		"mcpScopes":  sortedStrings(scopes),
		"ttlSeconds": plan.TTLSeconds.ValueInt64(),
		"enabled":    plan.Enabled.ValueBool(),
	}
	conditions := map[string][]string{}
	if !plan.Conditions.IsNull() && !plan.Conditions.IsUnknown() {
		diags.Append(plan.Conditions.ElementsAs(ctx, &conditions, false)...)
		for k, v := range conditions {
			conditions[k] = sortedStrings(v)
		}
	}
	args["conditions"] = conditions
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		args["description"] = plan.Description.ValueString()
	}
	if known(plan.Principal) {
		args["principal"] = plan.Principal.ValueString()
	}
	return args
}

func sortedStrings(s []string) []string {
	out := append([]string{}, s...)
	slices.Sort(out)
	return slices.Compact(out)
}

func (r *workloadBindingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workloadBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}
	args := bindingArgs(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	args["idempotencyKey"] = newIdempotencyKey()
	created, err := pmonmcp.Call[pmonmcp.WorkloadBinding](ctx, r.client, "create_workload_binding", args)
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the pmon workload binding "+plan.Name.ValueString(), err.Error())
		return
	}
	resp.Diagnostics.Append(applyWorkloadBinding(ctx, &plan, created)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workloadBindingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workloadBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}
	found, err := findWorkloadBinding(ctx, r.client, state.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot list pmon workload bindings", err.Error())
		return
	}
	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(applyWorkloadBinding(ctx, &state, *found)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func findWorkloadBinding(ctx context.Context, client *pmonmcp.Client, name string) (*pmonmcp.WorkloadBinding, error) {
	listed, err := pmonmcp.Call[pmonmcp.WorkloadBindings](ctx, client, "list_workload_bindings", map[string]any{})
	if err != nil {
		return nil, err
	}
	for _, b := range listed.Bindings {
		if b.Name == name {
			return &b, nil
		}
	}
	return nil, nil
}

func (r *workloadBindingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workloadBindingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}
	args := bindingArgs(ctx, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	args["idempotencyKey"] = newIdempotencyKey()
	updated, err := pmonmcp.Call[pmonmcp.WorkloadBinding](ctx, r.client, "update_workload_binding", args)
	if err != nil {
		resp.Diagnostics.AddError("Cannot update the pmon workload binding "+plan.Name.ValueString(), err.Error())
		return
	}
	resp.Diagnostics.Append(applyWorkloadBinding(ctx, &plan, updated)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *workloadBindingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workloadBindingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || r.client == nil {
		return
	}
	name := state.Name.ValueString()
	err := pmonmcp.Do(ctx, r.client, "delete_workload_binding", map[string]any{
		"name":           name,
		"idempotencyKey": newIdempotencyKey(),
	})
	if err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Cannot delete the pmon workload binding "+name, err.Error())
	}
}

func (r *workloadBindingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

func applyWorkloadBinding(ctx context.Context, model *workloadBindingModel, b pmonmcp.WorkloadBinding) diag.Diagnostics {
	var diags diag.Diagnostics
	model.ID = types.Int64Value(b.ID)
	model.Name = types.StringValue(b.Name)
	if b.Description != nil {
		model.Description = types.StringValue(*b.Description)
	} else if !model.Description.IsNull() {
		model.Description = types.StringNull()
	}
	model.Issuer = types.StringValue(b.Issuer)
	model.Subject = types.StringValue(b.Subject)
	model.Principal = types.StringValue(b.Principal)
	model.TTLSeconds = types.Int64Value(b.TTLSeconds)
	model.Enabled = types.BoolValue(b.Enabled)

	purposes, d := types.SetValueFrom(ctx, types.StringType, sortedStrings(b.Purposes))
	diags.Append(d...)
	model.Purposes = purposes
	scopes, d := types.SetValueFrom(ctx, types.StringType, sortedStrings(b.MCPScopes))
	diags.Append(d...)
	model.MCPScopes = scopes

	if len(b.Conditions) == 0 && (model.Conditions.IsNull() || model.Conditions.IsUnknown()) {
		model.Conditions = types.MapNull(conditionValuesType)
	} else {
		conditions, d := types.MapValueFrom(ctx, conditionValuesType, b.Conditions)
		diags.Append(d...)
		model.Conditions = conditions
	}
	return diags
}

type principalFromName struct{}

func (principalFromName) Description(context.Context) string {
	return "Defaults to svc: followed by the planned binding name."
}

func (m principalFromName) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (principalFromName) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}
	var name types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("name"), &name)...)
	if known(name) {
		resp.PlanValue = types.StringValue("svc:" + name.ValueString())
	} else {
		resp.PlanValue = types.StringUnknown()
	}
}

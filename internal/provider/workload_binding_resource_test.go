package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

func bindingModel(edit func(*workloadBindingModel)) workloadBindingModel {
	m := workloadBindingModel{
		ID:          types.Int64Unknown(),
		Name:        types.StringValue("tf-pmon-apply"),
		Description: types.StringNull(),
		Issuer:      types.StringValue("hcp-terraform"),
		Subject:     types.StringValue("organization:acme:project:infra:workspace:pmon:run_phase:apply"),
		Conditions:  types.MapNull(conditionValuesType),
		Principal:   types.StringUnknown(),
		Purposes:    scopeSet("mcp"),
		MCPScopes:   scopeSet("mcp:read", "mcp:policies:write"),
		TTLSeconds:  types.Int64Value(900),
		Enabled:     types.BoolValue(true),
	}
	if edit != nil {
		edit(&m)
	}
	return m
}

func conditionMap(t *testing.T, values map[string][]string) types.Map {
	t.Helper()
	m, diags := types.MapValueFrom(context.Background(), conditionValuesType, values)
	if diags.HasError() {
		t.Fatal(diags)
	}
	return m
}

func TestWorkloadBindingSchemaIsValidAndSetTyped(t *testing.T) {
	var resp resource.SchemaResponse
	NewWorkloadBindingResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if diags := resp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("schema: %v", diags)
	}
	for _, name := range []string{"purposes", "mcp_scopes"} {
		if _, isSet := resp.Schema.Attributes[name].GetType().(basetypes.SetType); !isSet {
			t.Errorf("%s is %s, want a set", name, resp.Schema.Attributes[name].GetType())
		}
	}
}

func TestValidateWorkloadBinding(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*workloadBindingModel)
		want string
	}{
		{"valid", nil, ""},
		{"wire only", func(m *workloadBindingModel) {
			m.Purposes, m.MCPScopes = scopeSet("wire"), types.SetValueMust(types.StringType, []attr.Value{})
		}, ""},
		{"bad name", func(m *workloadBindingModel) { m.Name = types.StringValue("TF Apply") }, "Invalid workload binding name"},
		{"lone star", func(m *workloadBindingModel) { m.Subject = types.StringValue("*") }, "Subject matches every token"},
		{"user principal", func(m *workloadBindingModel) { m.Principal = types.StringValue("alice@example.com") }, "Invalid service principal"},
		{"ttl", func(m *workloadBindingModel) { m.TTLSeconds = types.Int64Value(30) }, "TTL out of range"},
		{"purpose", func(m *workloadBindingModel) { m.Purposes = scopeSet("admin") }, "Unknown purpose"},
		{"mcp without scopes", func(m *workloadBindingModel) { m.MCPScopes = types.SetValueMust(types.StringType, []attr.Value{}) }, "disagree"},
		{"scopes without mcp", func(m *workloadBindingModel) { m.Purposes = scopeSet("wire") }, "disagree"},
		{"unknown scope", func(m *workloadBindingModel) { m.MCPScopes = scopeSet("mcp:root") }, "Unknown pmon scope"},
		{"unknown purposes are left for apply", func(m *workloadBindingModel) { m.Purposes = types.SetUnknown(types.StringType) }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateWorkloadBinding(context.Background(), bindingModel(tc.edit))
			if got := summaries(diags); (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
				t.Errorf("diagnostics %q, want %q", got, tc.want)
			}
		})
	}
	bad := bindingModel(nil)
	bad.Conditions = conditionMap(t, map[string][]string{"terraform_run_phase": {"*"}})
	if got := summaries(validateWorkloadBinding(context.Background(), bad)); !strings.Contains(got, "Invalid condition") {
		t.Errorf("condition: %q", got)
	}
}

func summaries(diags diag.Diagnostics) string {
	var out []string
	for _, d := range diags.Errors() {
		out = append(out, d.Summary())
	}
	return strings.Join(out, "; ")
}

func TestBindingArgsAreSortedAndOmitAnUnknownPrincipal(t *testing.T) {
	m := bindingModel(func(m *workloadBindingModel) {
		m.Conditions = conditionMap(t, map[string][]string{"terraform_run_phase": {"apply"}})
	})
	var diags diag.Diagnostics
	args := bindingArgs(context.Background(), m, &diags)
	raw, _ := json.Marshal(args)
	want := `{"conditions":{"terraform_run_phase":["apply"]},"enabled":true,"issuer":"hcp-terraform","mcpScopes":["mcp:policies:write","mcp:read"],` +
		`"name":"tf-pmon-apply","purposes":["mcp"],"subject":"organization:acme:project:infra:workspace:pmon:run_phase:apply","ttlSeconds":900}`
	if diags.HasError() || string(raw) != want {
		t.Errorf("args %s %v", raw, diags)
	}
}

func TestApplyWorkloadBindingKeepsAnUnsetConditionsNull(t *testing.T) {
	m := bindingModel(nil)
	diags := applyWorkloadBinding(context.Background(), &m, pmonmcp.WorkloadBinding{ID: 7, Name: "tf-pmon-apply", Issuer: "hcp-terraform",
		Subject: "organization:acme:project:infra:workspace:pmon:run_phase:apply", Conditions: map[string][]string{},
		Principal: "svc:tf-pmon-apply", Purposes: []string{"mcp"}, MCPScopes: []string{"mcp:read"}, TTLSeconds: 600, Enabled: true})
	if diags.HasError() || !m.Conditions.IsNull() || m.Principal.ValueString() != "svc:tf-pmon-apply" || m.ID.ValueInt64() != 7 || m.TTLSeconds.ValueInt64() != 600 {
		t.Errorf("model %+v %v", m, diags)
	}
}

func TestDefaultPrincipalFollowsThePlannedName(t *testing.T) {
	ctx := context.Background()
	var sresp resource.SchemaResponse
	NewWorkloadBindingResource().Schema(ctx, resource.SchemaRequest{}, &sresp)
	if principal, ok := sresp.Schema.Attributes["principal"].(schema.StringAttribute); !ok || len(principal.PlanModifiers) != 1 || principal.PlanModifiers[0] != (principalFromName{}) {
		t.Fatalf("principal attribute %#v", sresp.Schema.Attributes["principal"])
	}
	plan := func(name types.String) tfsdk.Plan {
		p := tfsdk.Plan{Schema: sresp.Schema}
		if diags := p.Set(ctx, bindingModel(func(m *workloadBindingModel) { m.Name = name })); diags.HasError() {
			t.Fatal(diags)
		}
		return p
	}
	for _, tc := range []struct {
		name   types.String
		config types.String
		want   types.String
	}{
		{types.StringValue("renamed"), types.StringNull(), types.StringValue("svc:renamed")},
		{types.StringUnknown(), types.StringNull(), types.StringUnknown()},
		{types.StringValue("renamed"), types.StringValue("svc:kept"), types.StringValue("svc:old")},
	} {
		req := planmodifier.StringRequest{Plan: plan(tc.name), ConfigValue: tc.config, StateValue: types.StringValue("svc:old"),
			PlanValue: types.StringValue("svc:old")}
		resp := &planmodifier.StringResponse{PlanValue: req.PlanValue}
		principalFromName{}.PlanModifyString(ctx, req, resp)
		if resp.Diagnostics.HasError() || !resp.PlanValue.Equal(tc.want) {
			t.Errorf("name %s config %s: planned %s %v, want %s", tc.name, tc.config, resp.PlanValue, resp.Diagnostics, tc.want)
		}
	}
}

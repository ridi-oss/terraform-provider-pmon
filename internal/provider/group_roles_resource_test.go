package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

func groupRolesTestClient(t *testing.T, groups []pmonmcp.Group, writes *[]string) *pmonmcp.Client {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "groups-test", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "list_groups", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{StructuredContent: map[string]any{"result": groups}}, nil
		})
	server.AddTool(&mcp.Tool{Name: "set_group_roles", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var input struct {
				GroupName string `json:"groupName"`
			}
			if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
				return nil, err
			}
			*writes = append(*writes, input.GroupName)
			return &mcp.CallToolResult{}, nil
		})
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(httpServer.Close)
	client, err := pmonmcp.Connect(context.Background(), pmonmcp.Options{Endpoint: httpServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func groupRolesTestState(t *testing.T, model groupRolesResourceModel) tfsdk.State {
	t.Helper()
	var schema resource.SchemaResponse
	NewGroupRolesResource().Schema(context.Background(), resource.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	if diags := state.Set(context.Background(), model); diags.HasError() {
		t.Fatal(diags)
	}
	return state
}

func TestGroupRolesRefreshFollowsIDAfterExternalRename(t *testing.T) {
	groups := []pmonmcp.Group{
		{ID: 42, Name: "new-name", Roles: []pmonmcp.RoleRef{{Name: "service:example"}}},
		{ID: 99, Name: "old-name", Roles: []pmonmcp.RoleRef{{Name: "system:admin"}}},
	}
	var writes []string
	r := &groupRolesResource{client: groupRolesTestClient(t, groups, &writes)}
	state := groupRolesTestState(t, groupRolesResourceModel{
		GroupID: types.Int64Value(42), GroupName: types.StringValue("old-name"), RoleNames: []string{"service:example"},
	})
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got groupRolesResourceModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got.GroupID.ValueInt64() != 42 || got.GroupName.ValueString() != "new-name" || len(got.RoleNames) != 1 || got.RoleNames[0] != "service:example" {
		t.Fatalf("refresh changed the binding identity or roles: %+v", got)
	}
	if len(writes) != 0 {
		t.Fatalf("refresh wrote roles: %v", writes)
	}
}

func TestGroupRolesRefreshPopulatesLegacyID(t *testing.T) {
	var writes []string
	r := &groupRolesResource{client: groupRolesTestClient(t, []pmonmcp.Group{{ID: 42, Name: "old-name"}}, &writes)}
	state := groupRolesTestState(t, groupRolesResourceModel{
		GroupID: types.Int64Null(), GroupName: types.StringValue("old-name"), RoleNames: []string{},
	})
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	var got groupRolesResourceModel
	if diags := resp.State.Get(context.Background(), &got); diags.HasError() {
		t.Fatal(diags)
	}
	if got.GroupID.ValueInt64() != 42 || len(writes) != 0 {
		t.Fatalf("legacy refresh did not learn the ID without writes: %+v, writes=%v", got, writes)
	}
}

func TestGroupRolesMissingIDDoesNotAdoptReusedName(t *testing.T) {
	var writes []string
	r := &groupRolesResource{client: groupRolesTestClient(t, []pmonmcp.Group{{ID: 99, Name: "old-name"}}, &writes)}
	state := groupRolesTestState(t, groupRolesResourceModel{
		GroupID: types.Int64Value(42), GroupName: types.StringValue("old-name"), RoleNames: []string{},
	})
	resp := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() || !resp.State.Raw.IsNull() {
		t.Fatalf("missing identity was not removed: %+v", resp)
	}
	var deleted resource.DeleteResponse
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &deleted)
	if deleted.Diagnostics.HasError() || len(writes) != 0 {
		t.Fatalf("delete touched the reused name: %v, %v", writes, deleted.Diagnostics)
	}
}

func TestGroupRolesSetRejectsChangedIdentityBeforeWriting(t *testing.T) {
	var writes []string
	r := &groupRolesResource{client: groupRolesTestClient(t, []pmonmcp.Group{{ID: 99, Name: "old-name"}}, &writes)}
	if _, err := r.set(context.Background(), "old-name", []string{}, types.Int64Value(42)); err == nil {
		t.Fatal("expected an identity mismatch error")
	}
	if len(writes) != 0 {
		t.Fatalf("identity check ran after the write: %v", writes)
	}
}

func TestGroupRolesDeleteClearsTheRenamedGroup(t *testing.T) {
	var writes []string
	r := &groupRolesResource{client: groupRolesTestClient(t, []pmonmcp.Group{
		{ID: 42, Name: "new-name"}, {ID: 99, Name: "old-name"},
	}, &writes)}
	state := groupRolesTestState(t, groupRolesResourceModel{
		GroupID: types.Int64Value(42), GroupName: types.StringValue("old-name"), RoleNames: []string{},
	})
	var resp resource.DeleteResponse
	r.Delete(context.Background(), resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() || len(writes) != 1 || writes[0] != "new-name" {
		t.Fatalf("delete did not follow the group ID: %v, %v", writes, resp.Diagnostics)
	}
}

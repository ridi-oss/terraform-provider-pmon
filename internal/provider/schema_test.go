package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// Order carries no meaning for either attribute, and a list compares element by element: Create
// stores the order the config was written in while Read stores pmon's, so a config not already in
// that order would show a diff on every plan and applying would never settle it.
func TestGroupRoleNamesIsASet(t *testing.T) {
	var resp resource.SchemaResponse
	NewGroupRolesResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)

	attr, ok := resp.Schema.Attributes["role_names"]
	if !ok {
		t.Fatal("pmon_group_roles has no role_names attribute")
	}
	if _, isSet := attr.GetType().(basetypes.SetType); !isSet {
		t.Errorf("role_names is %s, want a set", attr.GetType())
	}
}

func TestColumnClassificationTagsIsASet(t *testing.T) {
	var resp resource.SchemaResponse
	NewColumnClassificationResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)

	columns, ok := resp.Schema.Attributes["columns"].(schema.SetNestedAttribute)
	if !ok {
		t.Fatalf("columns is %T, want a set of nested objects", resp.Schema.Attributes["columns"])
	}
	tags, ok := columns.NestedObject.Attributes["tags"]
	if !ok {
		t.Fatal("a classified column has no tags attribute")
	}
	if _, isSet := tags.GetType().(basetypes.SetType); !isSet {
		t.Errorf("tags is %s, want a set", tags.GetType())
	}
}

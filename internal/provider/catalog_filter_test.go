package provider

import (
	"testing"

	"github.com/ridi-oss/terraform-provider-pmon/internal/pmonmcp"
)

func catalogRow(schema, table, column string) pmonmcp.CatalogColumn {
	return pmonmcp.CatalogColumn{Schema: schema, Table: table, Column: column}
}

// browse_catalog answers with the whole catalog and takes no filter arguments, so everything the
// filter lets through lands in Terraform state.
func TestCatalogFilter(t *testing.T) {
	rows := []pmonmcp.CatalogColumn{
		catalogRow("example", "user", "email"),
		catalogRow("example", "foo", "foo_email"),
		catalogRow("information_schema", "COLUMNS", "TABLE_NAME"),
		catalogRow("mysql", "user", "Host"),
	}

	for name, tc := range map[string]struct {
		filter catalogFilter
		want   []string
	}{
		"system schemas dropped by default": {
			catalogFilter{},
			[]string{"example.user.email", "example.foo.foo_email"},
		},
		"system schemas kept on request": {
			catalogFilter{IncludeSystemSchemas: true},
			[]string{"example.user.email", "example.foo.foo_email", "information_schema.COLUMNS.TABLE_NAME", "mysql.user.Host"},
		},
		"schema filter": {
			catalogFilter{Schema: "example"},
			[]string{"example.user.email", "example.foo.foo_email"},
		},
		"schema and table filter": {
			catalogFilter{Schema: "example", Table: "user"},
			[]string{"example.user.email"},
		},
		// Asking for a system schema by name and getting nothing back would be its own surprise.
		"named system schema is honoured": {
			catalogFilter{Schema: "information_schema"},
			[]string{"information_schema.COLUMNS.TABLE_NAME"},
		},
		"table filter alone": {
			catalogFilter{Table: "user"},
			[]string{"example.user.email"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var kept []string
			for _, row := range rows {
				if tc.filter.keep(row) {
					kept = append(kept, row.Schema+"."+row.Table+"."+row.Column)
				}
			}
			if len(kept) != len(tc.want) {
				t.Fatalf("kept %v, want %v", kept, tc.want)
			}
			for i := range kept {
				if kept[i] != tc.want[i] {
					t.Errorf("kept[%d] = %s, want %s", i, kept[i], tc.want[i])
				}
			}
		})
	}
}

// An unclassified column has no classification object at all, and a null slice would render as a
// null attribute rather than an empty list.
func TestClassificationTags(t *testing.T) {
	if got := classificationTags(nil); got == nil || len(got) != 0 {
		t.Errorf("classificationTags(nil) = %v, want an empty slice", got)
	}
	got := classificationTags(&pmonmcp.Classification{Tags: []string{"pii"}})
	if len(got) != 1 || got[0] != "pii" {
		t.Errorf("classificationTags = %v", got)
	}
}

package pmonmcp

import (
	"encoding/json"
	"testing"
)

// Decoding is pinned against real pmon responses, because a silently renamed field would show up
// as an empty attribute rather than an error.
func TestDecodeDatasource(t *testing.T) {
	raw := `{"id":1,"name":"example-finance-rw","engine":"mysql","host":"h","port":3306,
		"dbName":"exampledb","tags":["system:production"],"defaultSchemas":["exampledb"],
		"catalogSyncedAt":"2026-08-10T03:58:35Z","lastSeenAt":"2026-08-10T03:58:35Z",
		"engineVersion":"8.0.44"}`

	var ds Datasource
	if err := json.Unmarshal([]byte(raw), &ds); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if ds.Name != "example-finance-rw" || ds.Port != 3306 || ds.DBName != "exampledb" {
		t.Errorf("datasource = %+v", ds)
	}
	if len(ds.Tags) != 1 || ds.Tags[0] != "system:production" {
		t.Errorf("tags = %v", ds.Tags)
	}
}

// A datasource whose catalog never synced omits the field entirely, and that has to stay
// distinguishable from a zero time.
func TestDecodeDatasourceWithoutCatalog(t *testing.T) {
	var ds Datasource
	if err := json.Unmarshal([]byte(`{"name":"example-infosec-rw","defaultSchemas":[]}`), &ds); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if ds.CatalogSyncedAt != nil {
		t.Errorf("catalogSyncedAt = %v, want nil for a datasource that never synced", *ds.CatalogSyncedAt)
	}
}

func TestDecodePolicyOrigin(t *testing.T) {
	var shipped Policy
	if err := json.Unmarshal([]byte(`{"id":-250,"origin":"SYSTEM","systemKey":"preset.production-connect","name":"system:production-connect","cedarSrc":"permit();","enabled":true}`), &shipped); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !shipped.IsSystem() {
		t.Error("a SYSTEM policy must report IsSystem")
	}

	var authored Policy
	if err := json.Unmarshal([]byte(`{"id":1,"origin":"USER","name":"mine","cedarSrc":"permit();","enabled":true}`), &authored); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if authored.IsSystem() {
		t.Error("a USER policy must not report IsSystem")
	}
	if authored.SystemKey != nil {
		t.Error("a USER policy has no system key")
	}
}

func TestDecodeGroupRoles(t *testing.T) {
	raw := `{"id":10,"name":"developers","source":"OIDC","memberCount":33,
		"roles":[{"id":7,"name":"system:production-viewer"},{"id":2,"name":"system:development-viewer"}]}`

	var group Group
	if err := json.Unmarshal([]byte(raw), &group); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if group.MemberCount != 33 {
		t.Errorf("memberCount = %d", group.MemberCount)
	}
	names := group.RoleNames()
	if len(names) != 2 || names[0] != "system:production-viewer" {
		t.Errorf("RoleNames = %v", names)
	}
	if group.IsSystem() {
		t.Error("an OIDC group is not a SYSTEM group")
	}
}

// Group membership is only readable from the user side, so InGroup is what every membership read
// ends up going through.
func TestUserInGroup(t *testing.T) {
	raw := `{"id":1,"principal":"a@example.com","source":"OIDC","active":true,
		"groups":[{"id":10,"name":"developers"},{"id":2,"name":"system:admin"}]}`

	var user User
	if err := json.Unmarshal([]byte(raw), &user); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !user.InGroup("developers") || !user.InGroup("system:admin") {
		t.Errorf("groups = %+v", user.Groups)
	}
	if user.InGroup("nobody") {
		t.Error("InGroup matched a group the user is not in")
	}
}

// list_column_tags answers with the datasource and no mask function. Pinned against a real
// response so a field rename shows up as a failing test rather than an empty attribute.
func TestDecodeColumnTag(t *testing.T) {
	raw := `{"datasource":"example-prod-ro","schema":"example","table":"foo",
		"column":"foo_email","tags":["pii"]}`

	var tag ColumnTag
	if err := json.Unmarshal([]byte(raw), &tag); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if tag.Datasource != "example-prod-ro" || tag.Schema != "example" || tag.Table != "foo" {
		t.Errorf("column tag = %+v", tag)
	}
	if len(tag.Tags) != 1 || tag.Tags[0] != "pii" {
		t.Errorf("tags = %v", tag.Tags)
	}
}

// Pinned against a real browse_catalog row. Despite the tool's name the response is flat, and the
// classification is present only on a classified column.
func TestDecodeCatalogColumn(t *testing.T) {
	raw := `{"catalog":"def","schema":"example","table":"baz","column":"baz_id",
		"dataType":"varchar","sqlType":"VARCHAR","ordinal":5,"nullable":false,
		"classification":{"schema":"example","table":"baz","column":"baz_id","tags":["pii"]},
		"isTemp":false}`

	var column CatalogColumn
	if err := json.Unmarshal([]byte(raw), &column); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if column.Schema != "example" || column.Column != "baz_id" || column.Ordinal != 5 {
		t.Errorf("catalog column = %+v", column)
	}
	if column.Classification == nil || len(column.Classification.Tags) != 1 {
		t.Fatalf("classification = %+v", column.Classification)
	}

	var plain CatalogColumn
	if err := json.Unmarshal([]byte(`{"schema":"example","table":"bar","column":"id"}`), &plain); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if plain.Classification != nil {
		t.Error("an unclassified column must carry no classification")
	}
}

func TestDecodeDatasourceLiveness(t *testing.T) {
	raw := `{"datasource":"example-prod-ro","attached":true,
		"catalogSyncedAt":"2026-08-10T06:05:09Z","lastSeenAt":"2026-08-10T06:05:09Z"}`

	var liveness DatasourceLiveness
	if err := json.Unmarshal([]byte(raw), &liveness); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !liveness.Attached || liveness.Datasource != "example-prod-ro" {
		t.Errorf("liveness = %+v", liveness)
	}
}

func TestDecodeTableDetail(t *testing.T) {
	raw := `{"schema":"example","table":"foo","columns":[
		{"name":"id","dataType":"int","ordinal":1,"nullable":false,"partOfIndex":true,
		 "autoIncrement":true,"comment":""},
		{"name":"foo_email","dataType":"varchar","ordinal":6,"nullable":false,
		 "characterMaximumLength":255,"partOfIndex":true,"autoIncrement":false,
		 "comment":"SSO user email",
		 "classification":{"schema":"example","table":"foo","column":"foo_email","tags":["pii"]}}],
		"foreignKeys":[{"name":"FK_1","sourceSchema":"example","sourceTable":"foo",
		 "sourceColumns":["user_id"],"targetSchema":"example","targetTable":"user",
		 "targetColumns":["id"],"onUpdate":"NO ACTION","onDelete":"NO ACTION"}],
		"metadata":{"engine":"InnoDB","estimatedRows":120000,"rowFormat":"Dynamic",
		 "onDiskBytes":41943040,"collation":"utf8mb4_general_ci","comment":""}}`

	var detail TableDetail
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(detail.Columns) != 2 {
		t.Fatalf("columns = %d, want 2", len(detail.Columns))
	}
	if detail.Columns[0].Classification != nil {
		t.Error("an unclassified column must carry no classification")
	}
	if detail.Columns[1].Classification == nil {
		t.Fatal("the classified column lost its classification")
	}
	if len(detail.ForeignKeys) != 1 || detail.ForeignKeys[0].TargetTable != "user" {
		t.Errorf("foreign keys = %+v", detail.ForeignKeys)
	}
	if detail.Metadata == nil || detail.Metadata.EstimatedRows != 120000 {
		t.Errorf("metadata = %+v", detail.Metadata)
	}
}

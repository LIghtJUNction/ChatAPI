package service

import "testing"

func TestBuildAdminRequestsSchema(t *testing.T) {
	schema := BuildAdminRequestsSchema()
	if len(schema.Operations) != 1 {
		t.Fatalf("unexpected admin requests schema operations: %#v", schema)
	}
	operation := schema.Operations[0]
	if operation.Name != "requests_overview" || operation.Method != "GET" || operation.Path != "/api/admin/requests/overview" {
		t.Fatalf("unexpected admin requests schema operation: %#v", operation)
	}
	if len(operation.Notes) == 0 {
		t.Fatalf("expected admin requests schema notes: %#v", operation)
	}
}

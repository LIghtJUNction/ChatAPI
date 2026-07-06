package service

import "testing"

func TestBuildAdminAuditSchema(t *testing.T) {
	schema := BuildAdminAuditSchema()
	if len(schema.Operations) != 1 {
		t.Fatalf("unexpected admin audit schema operations: %#v", schema)
	}
	operation := schema.Operations[0]
	if operation.Name != "list_audit_logs" || operation.Method != "GET" || operation.Path != "/api/admin/audit/logs" {
		t.Fatalf("unexpected admin audit schema operation: %#v", operation)
	}
	if len(operation.Fields) != 4 {
		t.Fatalf("unexpected admin audit schema fields: %#v", operation)
	}
}

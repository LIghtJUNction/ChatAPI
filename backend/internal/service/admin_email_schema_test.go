package service

import "testing"

func TestBuildAdminEmailSchema(t *testing.T) {
	schema := BuildAdminEmailSchema()
	if len(schema.Operations) != 1 {
		t.Fatalf("unexpected admin email schema operations: %#v", schema)
	}
	operation := schema.Operations[0]
	if operation.Name != "send_test_email" || operation.Path != "/api/admin/send-test-email" {
		t.Fatalf("unexpected admin email schema operation: %#v", operation)
	}
	if len(operation.Fields) != 1 || operation.Fields[0].Key != "email" {
		t.Fatalf("unexpected admin email schema fields: %#v", operation)
	}
}

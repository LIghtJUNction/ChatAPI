package service

import "testing"

func TestBuildAdminRuntimeSchema(t *testing.T) {
	schema := BuildAdminRuntimeSchema()
	if len(schema.Operations) != 3 {
		t.Fatalf("unexpected admin runtime schema operations: %#v", schema)
	}
	if schema.Operations[0].Name != "automation_diagnostics" {
		t.Fatalf("unexpected first runtime operation: %#v", schema.Operations[0])
	}
	if schema.Operations[1].Name != "update_runtime_settings" || len(schema.Operations[1].Fields) != 2 {
		t.Fatalf("unexpected runtime settings operation: %#v", schema.Operations[1])
	}
}

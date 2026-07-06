package service

import "testing"

func TestBuildAdminConfigSchema(t *testing.T) {
	schema := BuildAdminConfigSchema()
	if len(schema.Operations) != 2 {
		t.Fatalf("unexpected admin config schema operations: %#v", schema)
	}
	if schema.Operations[1].Name != "set_config" || !schema.Operations[1].AllowUnknownTop {
		t.Fatalf("unexpected admin config set schema: %#v", schema.Operations[1])
	}
	if len(schema.Operations[1].Fields) != 1 {
		t.Fatalf("unexpected admin config schema fields: %#v", schema.Operations[1])
	}
}

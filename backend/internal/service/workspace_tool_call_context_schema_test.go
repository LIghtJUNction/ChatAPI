package service

import "testing"

func TestBuildWorkspaceToolCallContextSchema(t *testing.T) {
	schema := BuildWorkspaceToolCallContextSchema()
	if len(schema.Operations) != 1 {
		t.Fatalf("unexpected workspace tool-call context schema operations: %#v", schema)
	}
	operation := schema.Operations[0]
	if operation.Name != "assist_context" || operation.Path != "/api/workspace/tool-call/assist-context" {
		t.Fatalf("unexpected workspace tool-call context schema operation: %#v", operation)
	}
	if len(operation.Fields) != 3 {
		t.Fatalf("unexpected workspace tool-call context schema fields: %#v", operation)
	}
}

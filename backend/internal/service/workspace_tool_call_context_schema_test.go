package service

import "testing"

func TestBuildWorkspaceToolCallContextSchema(t *testing.T) {
	schema := BuildWorkspaceToolCallContextSchema()
	if len(schema.Operations) != 3 {
		t.Fatalf("unexpected workspace tool-call context schema operations: %#v", schema)
	}
	operation := schema.Operations[0]
	if operation.Name != "assist_context" || operation.Path != "/api/workspace/tool-call/assist-context" {
		t.Fatalf("unexpected workspace tool-call context schema operation: %#v", operation)
	}
	if len(operation.Fields) != 3 {
		t.Fatalf("unexpected workspace tool-call context schema fields: %#v", operation)
	}
	assistOperation := schema.Operations[1]
	if assistOperation.Name != "assist_execute" || assistOperation.Path != "/api/workspace/tool-call/assist" {
		t.Fatalf("unexpected workspace tool-call assist operation: %#v", assistOperation)
	}
	if len(assistOperation.Fields) != 4 {
		t.Fatalf("unexpected workspace tool-call assist fields: %#v", assistOperation)
	}
	streamOperation := schema.Operations[2]
	if streamOperation.Name != "assist_stream" || streamOperation.Path != "/api/workspace/tool-call/assist/stream" {
		t.Fatalf("unexpected workspace tool-call assist stream operation: %#v", streamOperation)
	}
	if len(streamOperation.Fields) != 4 {
		t.Fatalf("unexpected workspace tool-call assist stream fields: %#v", streamOperation)
	}
}

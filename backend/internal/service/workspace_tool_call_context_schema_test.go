package service

import "testing"

func TestBuildWorkspaceToolCallContextSchema(t *testing.T) {
	schema := BuildWorkspaceToolCallContextSchema()
	if len(schema.Operations) != 4 {
		t.Fatalf("unexpected workspace tool-call context schema operations: %#v", schema)
	}
	operation := schema.Operations[0]
	if operation.Name != "assist_context" || operation.Path != "/api/workspace/tool-call/assist-context" {
		t.Fatalf("unexpected workspace tool-call context schema operation: %#v", operation)
	}
	if len(operation.Fields) != 3 {
		t.Fatalf("unexpected workspace tool-call context schema fields: %#v", operation)
	}
	foundProvidersSection := false
	for _, section := range operation.ResponseSections {
		if section == "backend_assistant_providers" {
			foundProvidersSection = true
			break
		}
	}
	if !foundProvidersSection {
		t.Fatalf("expected backend_assistant_providers in assist-context schema: %#v", operation)
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
	parseOperation := schema.Operations[3]
	if parseOperation.Name != "assist_parse" || parseOperation.Path != "/api/workspace/tool-call/assist/parse" {
		t.Fatalf("unexpected workspace tool-call assist parse operation: %#v", parseOperation)
	}
	if len(parseOperation.Fields) != 5 {
		t.Fatalf("unexpected workspace tool-call assist parse fields: %#v", parseOperation)
	}
}

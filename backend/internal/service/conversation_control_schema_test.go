package service

import "testing"

func TestBuildConversationControlSchema(t *testing.T) {
	schema := BuildConversationControlSchema()
	if len(schema.Operations) != 7 {
		t.Fatalf("unexpected conversation control schema operations: %#v", schema)
	}
	if schema.Operations[1].Name != "respond_conversation" || len(schema.Operations[1].Fields) != 7 {
		t.Fatalf("unexpected respond conversation schema: %#v", schema.Operations[1])
	}
	if schema.Operations[5].Name != "legacy_output_delta" || schema.Operations[6].Name != "legacy_output_complete" {
		t.Fatalf("unexpected legacy output schema: %#v", schema)
	}
}

package service

import "testing"

func TestBuildToolCallAssistSchema(t *testing.T) {
	schema := BuildToolCallAssistSchema()
	if len(schema.ConfidenceLevels) != 3 {
		t.Fatalf("unexpected confidence levels: %#v", schema)
	}
	if schema.ConfidenceLevels[1] != "medium" {
		t.Fatalf("unexpected confidence order: %#v", schema.ConfidenceLevels)
	}
	required, ok := schema.OutputJSONSchema["required"].([]string)
	if !ok || len(required) != 2 || required[0] != "explanation" {
		t.Fatalf("unexpected output required fields: %#v", schema.OutputJSONSchema)
	}
	if len(schema.ValidationRules) == 0 || len(schema.Notes) == 0 {
		t.Fatalf("unexpected empty validation metadata: %#v", schema)
	}
}

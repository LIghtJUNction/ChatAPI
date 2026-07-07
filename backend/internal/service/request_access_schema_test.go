package service

import "testing"

func TestBuildLabRequestsSchema(t *testing.T) {
	schema := BuildLabRequestsSchema()
	if len(schema.Operations) != 6 {
		t.Fatalf("unexpected lab requests schema operations: %#v", schema)
	}
	if len(schema.ParsedItemFields) == 0 || len(schema.ParsedDetailFields) == 0 || len(schema.ReplayFields) == 0 {
		t.Fatalf("expected parsed/replay field metadata: %#v", schema)
	}
	if schema.Operations[2].Name != "copy_request_curl" || schema.Operations[3].Name != "request_delta" || schema.Operations[5].Name != "request_abort" {
		t.Fatalf("unexpected lab requests schema operations: %#v", schema)
	}
	if schema.ParsedDetailFields[11].Key != "request_method" || schema.ReplayFields[5].Key != "curl" {
		t.Fatalf("unexpected debug/replay field ordering: %#v", schema)
	}
	if len(schema.Authentication.Headers) != 0 || len(schema.ErrorCodes) != 0 || schema.Operations[0].ConsumesRateLimit {
		t.Fatalf("lab schema should not expose app api auth contract: %#v", schema)
	}
}

func TestBuildAppRequestsSchema(t *testing.T) {
	schema := BuildAppRequestsSchema()
	if len(schema.Operations) != 6 {
		t.Fatalf("unexpected app requests schema operations: %#v", schema)
	}
	if schema.Operations[0].Path != "/api/app/requests" ||
		schema.Operations[2].Path != "/api/app/requests/{request_id}/copy-curl" ||
		schema.Operations[4].Path != "/api/app/requests/{request_id}/complete" {
		t.Fatalf("unexpected app requests schema paths: %#v", schema)
	}
	if schema.ParsedItemFields[0].Key != "request_id" || schema.ParsedDetailFields[16].Key != "replay" {
		t.Fatalf("unexpected app parsed field metadata: %#v", schema)
	}
	if len(schema.Authentication.Headers) != 2 || len(schema.ErrorCodes) == 0 {
		t.Fatalf("expected auth and error metadata in app request schema: %#v", schema)
	}
	if !schema.Operations[0].ConsumesRateLimit || schema.Operations[3].ResponseShape == "" || len(schema.Operations[3].ResourceLimitKeys) == 0 {
		t.Fatalf("unexpected app request operation contract: %#v", schema.Operations[3])
	}
}

func TestBuildAppConversationsSchema(t *testing.T) {
	schema := BuildAppConversationsSchema()
	if len(schema.Operations) != 2 {
		t.Fatalf("unexpected app conversations schema operations: %#v", schema)
	}
	if schema.Operations[1].Name != "list_conversation_messages" {
		t.Fatalf("unexpected app conversations schema operations: %#v", schema)
	}
	if len(schema.Authentication.Headers) != 2 || len(schema.ErrorCodes) == 0 {
		t.Fatalf("expected auth and error metadata in app conversations schema: %#v", schema)
	}
	if schema.Operations[1].ResponseShape != "{ok, items}" || !schema.Operations[1].ConsumesRateLimit {
		t.Fatalf("unexpected app conversations operation contract: %#v", schema.Operations[1])
	}
}

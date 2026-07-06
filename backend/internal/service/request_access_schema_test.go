package service

import "testing"

func TestBuildLabRequestsSchema(t *testing.T) {
	schema := BuildLabRequestsSchema()
	if len(schema.Operations) != 5 {
		t.Fatalf("unexpected lab requests schema operations: %#v", schema)
	}
	if schema.Operations[2].Name != "request_delta" || schema.Operations[4].Name != "request_abort" {
		t.Fatalf("unexpected lab requests schema operations: %#v", schema)
	}
}

func TestBuildAppRequestsSchema(t *testing.T) {
	schema := BuildAppRequestsSchema()
	if len(schema.Operations) != 5 {
		t.Fatalf("unexpected app requests schema operations: %#v", schema)
	}
	if schema.Operations[0].Path != "/api/app/requests" || schema.Operations[3].Path != "/api/app/requests/{request_id}/complete" {
		t.Fatalf("unexpected app requests schema paths: %#v", schema)
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
}

package service

import "testing"

func TestBuildUserIdentitiesSchema(t *testing.T) {
	schema := BuildUserIdentitiesSchema()
	if len(schema.Operations) != 2 {
		t.Fatalf("unexpected user identities schema operations: %#v", schema)
	}
	if schema.Operations[0].Name != "list_identities" || schema.Operations[1].Name != "unlink_identity" {
		t.Fatalf("unexpected user identities schema operations: %#v", schema)
	}
}

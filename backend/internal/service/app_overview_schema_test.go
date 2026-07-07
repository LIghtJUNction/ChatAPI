package service

import "testing"

func TestBuildAppOverviewSchema(t *testing.T) {
	schema := BuildAppOverviewSchema()
	if len(schema.Operations) != 2 {
		t.Fatalf("unexpected app overview schema operations: %#v", schema)
	}
	if schema.Operations[0].Name != "me" || schema.Operations[1].Name != "statistics_summary" {
		t.Fatalf("unexpected app overview schema operations: %#v", schema)
	}
	if len(schema.Authentication.Headers) != 2 || len(schema.ErrorCodes) == 0 {
		t.Fatalf("expected auth and error metadata in app overview schema: %#v", schema)
	}
	if schema.Operations[0].ResponseShape != "{ok, app_api_key, user}" || !schema.Operations[0].ConsumesRateLimit {
		t.Fatalf("unexpected app overview operation contract: %#v", schema.Operations[0])
	}
}

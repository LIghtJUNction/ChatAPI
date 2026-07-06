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
}

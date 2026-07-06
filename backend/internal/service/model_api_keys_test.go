package service

import (
	"errors"
	"testing"
)

func TestModelAPIKeySchema(t *testing.T) {
	schema := NewModelAPIKeyService(nil, "test-master-key").Schema()
	if schema.KeyPrefix != "sk-" {
		t.Fatalf("unexpected model api key prefix: %#v", schema)
	}
	if len(schema.CreateFields) != 2 {
		t.Fatalf("unexpected model api key create fields: %#v", schema)
	}
	if schema.CreateFields[1].Name != "model" || !schema.CreateFields[1].Required {
		t.Fatalf("unexpected required model field: %#v", schema.CreateFields)
	}
}

func TestModelAPIKeyCreateRequiresModel(t *testing.T) {
	svc := NewModelAPIKeyService(nil, "test-master-key")
	_, _, err := svc.CreateKey(nil, "user_demo", "demo-key", "")
	if !errors.Is(err, ErrModelRequired) {
		t.Fatalf("expected ErrModelRequired, got %v", err)
	}
}

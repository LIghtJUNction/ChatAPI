package service

import (
	"testing"

	"github.com/zyf/chatapi/internal/config"
)

func TestUserConfigServiceSchema(t *testing.T) {
	schema := NewUserConfigService(nil).Schema()
	if !schema.AllowUnknownKeys {
		t.Fatalf("expected user config schema to allow unknown keys: %#v", schema)
	}
	if len(schema.ReservedPrefixes) != 1 || schema.ReservedPrefixes[0] != reservedUserConfigPrefix {
		t.Fatalf("unexpected reserved prefixes: %#v", schema)
	}
	if len(schema.Fields) != 4 {
		t.Fatalf("unexpected user config schema fields: %#v", schema)
	}
	if schema.Fields[0].Key != "ntfy_url_enabled" || schema.Fields[0].ValueType != "boolean" {
		t.Fatalf("unexpected first user config field: %#v", schema.Fields[0])
	}
	if schema.Fields[3].Key != "messages_per_minute_limit" {
		t.Fatalf("unexpected messages_per_minute_limit field: %#v", schema.Fields[3])
	}
	if min, ok := schema.Fields[3].Validation["min"].(int); !ok || min != 0 {
		t.Fatalf("unexpected messages_per_minute_limit validation: %#v", schema.Fields[3])
	}
}

func TestSystemSettingsServiceSchema(t *testing.T) {
	cfg := config.Config{
		SMTPEnabled:                   true,
		RealtimeMaxConnections:        64,
		RealtimeMaxConnectionsPerUser: 6,
		UploadMaxBytes:                2048,
		StorageDefaultQuotaBytes:      4096,
	}
	schema := NewSystemSettingsService(nil, cfg).Schema()
	if schema.AllowUnknownKeys {
		t.Fatalf("expected system settings schema to disallow unknown keys: %#v", schema)
	}
	if len(schema.Fields) == 0 {
		t.Fatalf("unexpected empty system settings schema: %#v", schema)
	}
	foundEmailProvider := false
	foundImageUsage := false
	foundStorageBlockNewConversations := false
	for _, field := range schema.Fields {
		switch field.Key {
		case "email_provider":
			foundEmailProvider = true
			values, ok := field.Validation["allowed_values"].([]string)
			if !ok || len(values) != 1 || values[0] != "smtp" {
				t.Fatalf("unexpected email provider validation: %#v", field)
			}
		case "image_usage":
			foundImageUsage = true
			if !field.ReadOnly {
				t.Fatalf("expected image_usage to be read only: %#v", field)
			}
		case "storage_block_new_conversations":
			foundStorageBlockNewConversations = true
		}
	}
	if !foundEmailProvider || !foundImageUsage || !foundStorageBlockNewConversations {
		t.Fatalf("expected email_provider and image_usage fields in schema: %#v", schema.Fields)
	}
}

package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
)

func TestVirtualModelServiceDefaultsUpsertsAndDeletes(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite: %v", err)
	}

	svc := NewVirtualModelService(st)
	ctx := context.Background()

	initial, err := svc.List(ctx, "user_models")
	if err != nil {
		t.Fatalf("list default virtual models: %v", err)
	}
	if len(initial) != 1 || initial[0].ID != "chatapi-lab" {
		t.Fatalf("unexpected default models: %#v", initial)
	}

	updated, err := svc.Upsert(ctx, "user_models", VirtualModel{
		ID:      "chatapi-demo",
		Name:    "ChatAPI Demo",
		OwnedBy: "tester",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert virtual model: %v", err)
	}
	if len(updated) != 2 {
		t.Fatalf("expected 2 virtual models after upsert: %#v", updated)
	}

	updated, err = svc.Delete(ctx, "user_models", "chatapi-demo")
	if err != nil {
		t.Fatalf("delete virtual model: %v", err)
	}
	if len(updated) != 1 || updated[0].ID != "chatapi-lab" {
		t.Fatalf("unexpected virtual models after delete: %#v", updated)
	}
}

func TestVirtualModelServiceSchema(t *testing.T) {
	schema := NewVirtualModelService(nil).Schema()
	if len(schema.CreateFields) != 5 {
		t.Fatalf("unexpected virtual model schema fields: %#v", schema)
	}
	if schema.CreateFields[0].Name != "id" || !schema.CreateFields[0].Required {
		t.Fatalf("unexpected virtual model id field: %#v", schema.CreateFields)
	}
	if schema.CreateFields[1].Name != "name" || !schema.CreateFields[1].Required {
		t.Fatalf("unexpected virtual model name field: %#v", schema.CreateFields)
	}
}

func TestVirtualModelServiceUpsertRequiresIDAndName(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite: %v", err)
	}

	svc := NewVirtualModelService(st)
	ctx := context.Background()

	_, err = svc.Upsert(ctx, "user_models", VirtualModel{Name: "Missing ID"})
	if !errors.Is(err, ErrVirtualModelIDRequired) {
		t.Fatalf("expected ErrVirtualModelIDRequired, got %v", err)
	}

	_, err = svc.Upsert(ctx, "user_models", VirtualModel{ID: "demo-model"})
	if !errors.Is(err, ErrVirtualModelNameRequired) {
		t.Fatalf("expected ErrVirtualModelNameRequired, got %v", err)
	}
}

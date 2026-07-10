package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
)

func TestCreateStagedMediaAssetRequiresConversationOwnership(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_owned", RequestID: "req_owned", ResponseID: "resp_owned",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "draw",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateStagedMediaAsset(ctx, common.CreateStagedMediaAssetInput{
		Asset: common.CreateMediaAssetInput{
			ID: "asset_wrong_owner", OwnerID: "user_b", FileID: "file_wrong_owner",
			Path: "/tmp/file_wrong_owner.avif", MediaType: "image/avif", CreatedAt: time.Now().UTC(),
		},
		ConversationID: conversation.ID,
		RequestID:      "req_owned",
	})
	if !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected owner-scoped not found, got %v", err)
	}
	var count int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM media_assets WHERE id = ?`, "asset_wrong_owner").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unauthorized staged asset was committed: %d", count)
	}
}

func TestCreateStagedMediaAssetRequiresCurrentRequest(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_reused", RequestID: "req_old", ResponseID: "resp_old",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "old",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CompletePendingTurn(ctx, common.CompletePendingInput{
		ConversationID: conversation.ID, ResponseID: "resp_old", OutputText: "done", Mode: "answer",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: conversation.ID, RequestID: "req_new", ResponseID: "resp_new", ReuseConversation: true,
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "new",
	}); err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateStagedMediaAsset(ctx, common.CreateStagedMediaAssetInput{
		Asset: common.CreateMediaAssetInput{
			ID: "asset_old_request", OwnerID: "user_a", FileID: "file_old_request",
			Path: "/tmp/file_old_request.avif", MediaType: "image/avif", CreatedAt: time.Now().UTC(),
		},
		ConversationID: conversation.ID,
		RequestID:      "req_old",
	})
	if !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected stale request to be rejected, got %v", err)
	}
}

func TestAppendConversationEventWithAssetIsRecoverableAndReferenced(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "chat.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB().Close()
	if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
		t.Fatal(err)
	}
	conversation, _, err := store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID: "conv_image", RequestID: "req_image", ResponseID: "resp_image",
		OwnerID: "user_a", RequestFormat: "responses", Model: "gpt-4o", UserContent: "draw",
	})
	if err != nil {
		t.Fatal(err)
	}
	asset, err := store.CreateStagedMediaAsset(ctx, common.CreateStagedMediaAssetInput{
		Asset: common.CreateMediaAssetInput{
			ID: "asset_image", OwnerID: "user_a", FileID: "file_image", Path: "/tmp/file_image.avif",
			MediaType: "image/avif", Bytes: 10, SHA256: "sha", Width: 2, Height: 1,
			SourceKind: "output_upload", CreatedAt: time.Now().UTC().Add(-25 * time.Hour),
		},
		ConversationID: conversation.ID,
		RequestID:      "req_image",
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := store.AppendConversationEventWithAsset(ctx, common.AppendConversationEventWithAssetInput{
		AssetID:  asset.ID,
		AssetURL: "/api/media/assets/file_image",
		Purpose:  "image_generation_result",
		Event: common.AppendConversationEventInput{
			ConversationID: conversation.ID, OwnerID: "user_a", RequestID: "req_image",
			Type: "builtin_tool", Title: "Image Generation",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListConversationEvents(ctx, conversation.ID)
	if err != nil || len(items) != 1 || items[0].ID != event.ID || len(items[0].MediaAssets) != 1 || items[0].MediaAssets[0].URL != "/api/media/assets/file_image" {
		t.Fatalf("event was not recoverable: %#v err=%v", items, err)
	}
	orphans, err := store.ListOrphanMediaAssets(ctx)
	if err != nil || len(orphans) != 0 {
		t.Fatalf("referenced output asset reported as orphan: %#v err=%v", orphans, err)
	}
	var refCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM media_asset_event_refs WHERE event_id = ?`, event.ID).Scan(&refCount); err != nil || refCount != 1 {
		t.Fatalf("missing event asset ref: count=%d err=%v", refCount, err)
	}
}

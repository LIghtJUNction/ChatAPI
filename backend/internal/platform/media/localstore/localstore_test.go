package localstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/platform/media"
)

func TestPersistDraft(t *testing.T) {
	root := t.TempDir()
	store := Store{RootDir: root}
	asset, err := store.PersistDraft(context.Background(), media.DraftAsset{
		OwnerID:   "user/a",
		FileID:    "file_test",
		MediaType: "image/avif",
		Data:      []byte("avif-bytes"),
	})
	if err != nil {
		t.Fatalf("persist avif: %v", err)
	}
	if filepath.Dir(asset.Path) != filepath.Join(root, "user_a") {
		t.Fatalf("unexpected asset path: %#v", asset)
	}
	if filepath.Base(asset.Path) != "file_test.avif" {
		t.Fatalf("unexpected asset filename: %#v", asset)
	}
	if data, err := os.ReadFile(asset.Path); err != nil || string(data) != "avif-bytes" {
		t.Fatalf("unexpected persisted bytes: err=%v data=%q", err, string(data))
	}
}

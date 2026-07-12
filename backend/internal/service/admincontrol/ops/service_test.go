package ops

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/platform/media/localstore"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
)

func TestCleanupOrphanImagesDeletesFilesAndRecords(t *testing.T) {
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	root := filepath.Join(t.TempDir(), "derived")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	path := filepath.Join(root, "orphan.avif")
	if err := os.WriteFile(path, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	if _, err := st.DB().ExecContext(context.Background(), `
		INSERT INTO media_assets(id, owner_id, file_id, path, media_type, bytes, sha256, width, height, source_kind, original_name, original_media_type, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	`, "asset_orphan", "user_a", "file_orphan", path, "image/avif", 6, "abc", 1, 1, "base64", "", "image/png"); err != nil {
		t.Fatalf("insert media asset: %v", err)
	}

	svc := New(st, localstore.Store{RootDir: root})
	result, err := svc.CleanupOrphanImages(context.Background())
	if err != nil {
		t.Fatalf("cleanup orphan images: %v", err)
	}
	if result.Scanned != 1 || result.DeletedAssetRecords != 1 {
		t.Fatalf("unexpected cleanup result: %#v", result)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file deleted, stat err=%v", err)
	}
	items, err := st.ListOrphanMediaAssets(context.Background())
	if err != nil {
		t.Fatalf("list orphan media assets: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected no orphan assets left: %#v", items)
	}
}

var _ Store = (*sqlitestore.Store)(nil)
var _ = common.MediaAsset{}

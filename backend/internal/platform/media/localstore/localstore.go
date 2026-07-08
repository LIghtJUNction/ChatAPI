package localstore

import (
	"context"
	"fmt"
	"github.com/zyf/chatapi/internal/platform/media"
	"os"
	"path/filepath"
	"strings"
)

type StoredAsset struct {
	FileID    string
	Path      string
	MediaType string
	Bytes     int64
	Filename  string
}

type Store struct {
	RootDir string
}

func (s Store) PersistAVIF(_ context.Context, ownerID string, fileID string, parsed media.ParsedImage, avifBytes []byte) (StoredAsset, error) {
	root := strings.TrimSpace(s.RootDir)
	if root == "" {
		return StoredAsset{}, fmt.Errorf("localstore root dir is required")
	}
	ownerID = sanitizeSegment(ownerID)
	if ownerID == "" {
		ownerID = "anonymous"
	}
	dir := filepath.Join(root, ownerID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return StoredAsset{}, fmt.Errorf("create derived media dir: %w", err)
	}
	fileID = sanitizeSegment(fileID)
	if fileID == "" {
		return StoredAsset{}, fmt.Errorf("file id is required")
	}
	filename := fmt.Sprintf("%s.avif", fileID)
	fullPath := filepath.Join(dir, filename)
	if err := os.WriteFile(fullPath, avifBytes, 0o644); err != nil {
		return StoredAsset{}, fmt.Errorf("write avif file: %w", err)
	}
	return StoredAsset{
		FileID:    fileID,
		Path:      fullPath,
		MediaType: "image/avif",
		Bytes:     int64(len(avifBytes)),
		Filename:  filename,
	}, nil
}

func (s Store) DeletePreparedImage(_ context.Context, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete prepared image: %w", err)
	}
	return nil
}

func sanitizeSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, "..", "_")
	return value
}

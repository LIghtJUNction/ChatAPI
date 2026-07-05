package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

var ErrUploadNotFound = errors.New("upload not found")
var ErrInvalidUploadPath = errors.New("invalid upload path")
var ErrUnsupportedUploadType = errors.New("unsupported upload type")
var ErrUploadTooLarge = errors.New("upload too large")

type UploadService struct {
	root     string
	maxBytes int64
	store    store.Store
}

type UploadUsage struct {
	Root      string `json:"root"`
	Bytes     int64  `json:"bytes"`
	FileCount int    `json:"file_count"`
}

type UploadResult struct {
	ID               string `json:"id,omitempty"`
	OwnerID          string `json:"owner_id,omitempty"`
	Filename         string `json:"filename"`
	OriginalFilename string `json:"original_filename,omitempty"`
	URL              string `json:"url"`
	Bytes            int64  `json:"bytes"`
	ContentType      string `json:"content_type"`
}

func NewUploadService(cfg config.Config, dataStore store.Store) *UploadService {
	return &UploadService{
		root:     filepath.Join(cfg.DataDir, "uploads", "imgs"),
		maxBytes: cfg.UploadMaxBytes,
		store:    dataStore,
	}
}

func (s *UploadService) MaxRequestBytes() int64 {
	limit := s.maxBytes
	if limit <= 0 {
		limit = 10 << 20
	}
	return limit + 4096
}

var uploadExtensionsByMIME = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

func (s *UploadService) ResolveImagePath(filename string) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" || filename == "." || filename == ".." {
		return "", ErrInvalidUploadPath
	}
	if strings.ContainsAny(filename, `/\`) || filepath.Base(filename) != filename {
		return "", ErrInvalidUploadPath
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(root, filepath.Clean(filename)))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	if relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return "", ErrInvalidUploadPath
	}
	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrUploadNotFound
		}
		return "", err
	}
	if stat.IsDir() {
		return "", ErrUploadNotFound
	}
	return path, nil
}

func (s *UploadService) SaveImage(ctx context.Context, file multipart.File, originalFilename string) (UploadResult, error) {
	if file == nil {
		return UploadResult{}, ErrInvalidUploadPath
	}
	limit := s.maxBytes
	if limit <= 0 {
		limit = 10 << 20
	}
	reader := io.LimitReader(file, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return UploadResult{}, err
	}
	if int64(len(data)) > limit {
		return UploadResult{}, ErrUploadTooLarge
	}
	contentType := http.DetectContentType(data)
	extension, ok := uploadExtensionsByMIME[contentType]
	if !ok {
		return UploadResult{}, ErrUnsupportedUploadType
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return UploadResult{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return UploadResult{}, err
	}
	filename := uuid.NewString() + extension
	path, err := filepath.Abs(filepath.Join(root, filename))
	if err != nil {
		return UploadResult{}, err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return UploadResult{}, err
	}
	if strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return UploadResult{}, ErrInvalidUploadPath
	}
	select {
	case <-ctx.Done():
		return UploadResult{}, ctx.Err()
	default:
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return UploadResult{}, fmt.Errorf("write upload: %w", err)
	}
	result := UploadResult{
		Filename:         filename,
		OriginalFilename: cleanOriginalUploadFilename(originalFilename),
		URL:              "/api/uploads/imgs/" + filename,
		Bytes:            int64(len(data)),
		ContentType:      contentType,
	}
	if s.store == nil {
		return result, nil
	}
	ownerID := OwnerIDFromContext(ctx)
	if ownerID == "" {
		ownerID = "anonymous"
	}
	record, err := s.store.CreateUploadedImage(ctx, store.CreateUploadedImageInput{
		ID:               "upload_" + uuid.NewString(),
		OwnerID:          ownerID,
		Filename:         result.Filename,
		OriginalFilename: result.OriginalFilename,
		ContentType:      result.ContentType,
		Bytes:            result.Bytes,
		URL:              result.URL,
	})
	if err != nil {
		_ = os.Remove(path)
		return UploadResult{}, fmt.Errorf("record upload metadata: %w", err)
	}
	result.ID = record.ID
	result.OwnerID = record.OwnerID
	return result, nil
}

func cleanOriginalUploadFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return ""
	}
	filename = filepath.Base(filename)
	if filename == "." || filename == string(filepath.Separator) {
		return ""
	}
	return filename
}

func (s *UploadService) Usage(ctx context.Context) (UploadUsage, error) {
	usage := UploadUsage{Root: s.root}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return usage, err
	}
	usage.Root = root
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return usage, nil
		}
		return usage, err
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry == nil || entry.IsDir() {
			return nil
		}
		stat, err := entry.Info()
		if err != nil {
			return err
		}
		usage.FileCount++
		usage.Bytes += stat.Size()
		return nil
	})
	return usage, err
}

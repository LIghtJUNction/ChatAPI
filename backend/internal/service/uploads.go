package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/zyf/chatapi/internal/config"
)

var ErrUploadNotFound = errors.New("upload not found")
var ErrInvalidUploadPath = errors.New("invalid upload path")

type UploadService struct {
	root string
}

type UploadUsage struct {
	Root      string `json:"root"`
	Bytes     int64  `json:"bytes"`
	FileCount int    `json:"file_count"`
}

func NewUploadService(cfg config.Config) *UploadService {
	return &UploadService{root: filepath.Join(cfg.DataDir, "uploads", "imgs")}
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

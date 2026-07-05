package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

type StorageMonitorService struct {
	cfg   config.Config
	store store.Store
}

type StorageSummary struct {
	GeneratedAt       time.Time     `json:"generated_at"`
	Database          DatabaseInfo  `json:"database"`
	Uploads           DirectoryInfo `json:"uploads"`
	EstimatedUsers    int           `json:"estimated_users"`
	EstimatedBytes    int64         `json:"estimated_bytes"`
	ConversationCount int           `json:"conversation_count"`
	MessageCount      int           `json:"message_count"`
}

type DirectoryInfo struct {
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
	FileCount int    `json:"file_count"`
}

type UserStorageUsage struct {
	UserID            string `json:"user_id"`
	EstimatedBytes    int64  `json:"estimated_bytes"`
	ConversationCount int    `json:"conversation_count"`
	MessageCount      int    `json:"message_count"`
}

func NewStorageMonitorService(cfg config.Config, dataStore store.Store) *StorageMonitorService {
	return &StorageMonitorService{cfg: cfg, store: dataStore}
}

func (s *StorageMonitorService) Summary(ctx context.Context) (StorageSummary, error) {
	users, err := s.Users(ctx)
	if err != nil {
		return StorageSummary{}, err
	}
	var estimatedBytes int64
	var conversationCount int
	var messageCount int
	for _, item := range users {
		estimatedBytes += item.EstimatedBytes
		conversationCount += item.ConversationCount
		messageCount += item.MessageCount
	}
	return StorageSummary{
		GeneratedAt:       time.Now().UTC(),
		Database:          storageDatabaseInfo(s.cfg),
		Uploads:           directoryInfo(filepath.Join(s.cfg.DataDir, "uploads")),
		EstimatedUsers:    len(users),
		EstimatedBytes:    estimatedBytes,
		ConversationCount: conversationCount,
		MessageCount:      messageCount,
	}, nil
}

func (s *StorageMonitorService) Users(ctx context.Context) ([]UserStorageUsage, error) {
	conversations, err := s.store.ListConversations(ctx)
	if err != nil {
		return nil, err
	}
	byUser := map[string]*UserStorageUsage{}
	for _, conversation := range conversations {
		ownerID := stringValue(conversation.Metadata["owner_id"], "unknown")
		usage := byUser[ownerID]
		if usage == nil {
			usage = &UserStorageUsage{UserID: ownerID}
			byUser[ownerID] = usage
		}
		usage.ConversationCount++
		usage.EstimatedBytes += estimatedJSONBytes(conversation.Metadata)
		usage.EstimatedBytes += int64(len(conversation.Title) + len(conversation.LastMessagePreview) + len(conversation.LastUserText))

		messages, err := s.store.ListMessages(ctx, conversation.ID)
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			usage.MessageCount++
			usage.EstimatedBytes += int64(len(message.Content) + len(message.Role) + len(message.Status))
			usage.EstimatedBytes += estimatedJSONBytes(message.Metadata)
		}
	}
	items := make([]UserStorageUsage, 0, len(byUser))
	for _, item := range byUser {
		items = append(items, *item)
	}
	return items, nil
}

func storageDatabaseInfo(cfg config.Config) DatabaseInfo {
	info := DatabaseInfo{Driver: cfg.DatabaseDriver}
	if cfg.DatabaseDriver != "sqlite" {
		return info
	}
	info.SQLitePath = cfg.DatabaseDSN
	if stat, err := os.Stat(cfg.DatabaseDSN); err == nil {
		info.SQLiteBytes = stat.Size()
	}
	walPath := cfg.DatabaseDSN + "-wal"
	info.SQLiteWAL = walPath
	if stat, err := os.Stat(walPath); err == nil {
		info.SQLiteWALBytes = stat.Size()
	}
	return info
}

func directoryInfo(root string) DirectoryInfo {
	info := DirectoryInfo{Path: root}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		stat, err := entry.Info()
		if err != nil {
			return nil
		}
		info.FileCount++
		info.Bytes += stat.Size()
		return nil
	})
	return info
}

func estimatedJSONBytes(value any) int64 {
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return int64(len(data))
}

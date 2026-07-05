package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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
	StorageQuotaBytes int64  `json:"storage_quota_bytes"`
	StorageOverQuota  bool   `json:"storage_over_quota"`
	ConversationCount int    `json:"conversation_count"`
	MessageCount      int    `json:"message_count"`
	ImageCount        int    `json:"image_count"`
	ImageBytes        int64  `json:"image_bytes"`
}

type StorageCleanupPreviewInput struct {
	OwnerID                 string `json:"owner_id,omitempty"`
	KeepRecentConversations int    `json:"keep_recent_conversations"`
	KeepRecentDays          int    `json:"keep_recent_days"`
}

type StorageCleanupPreview struct {
	GeneratedAt               time.Time                 `json:"generated_at"`
	DryRun                    bool                      `json:"dry_run"`
	OwnerID                   string                    `json:"owner_id,omitempty"`
	KeepRecentConversations   int                       `json:"keep_recent_conversations"`
	KeepRecentDays            int                       `json:"keep_recent_days"`
	CandidateConversations    int                       `json:"candidate_conversations"`
	CandidateMessages         int                       `json:"candidate_messages"`
	EstimatedReclaimableBytes int64                     `json:"estimated_reclaimable_bytes"`
	ByOwner                   []StorageCleanupOwnerPlan `json:"by_owner"`
}

type StorageCleanupOwnerPlan struct {
	OwnerID                   string `json:"owner_id"`
	CandidateConversations    int    `json:"candidate_conversations"`
	CandidateMessages         int    `json:"candidate_messages"`
	EstimatedReclaimableBytes int64  `json:"estimated_reclaimable_bytes"`
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
	images, err := s.store.ListUploadedImages(ctx)
	if err != nil {
		return nil, err
	}
	for _, image := range images {
		ownerID := stringValue(image.OwnerID, "unknown")
		usage := byUser[ownerID]
		if usage == nil {
			usage = &UserStorageUsage{UserID: ownerID}
			byUser[ownerID] = usage
		}
		usage.ImageCount++
		usage.ImageBytes += image.Bytes
		usage.EstimatedBytes += image.Bytes
	}
	items := make([]UserStorageUsage, 0, len(byUser))
	for _, item := range byUser {
		item.StorageQuotaBytes = s.cfg.StorageDefaultQuotaBytes
		item.StorageOverQuota = item.StorageQuotaBytes > 0 && item.EstimatedBytes > item.StorageQuotaBytes
		items = append(items, *item)
	}
	return items, nil
}

func (s *StorageMonitorService) CleanupPreview(ctx context.Context, input StorageCleanupPreviewInput) (StorageCleanupPreview, error) {
	conversations, err := s.store.ListConversations(ctx)
	if err != nil {
		return StorageCleanupPreview{}, err
	}
	if input.KeepRecentConversations < 0 {
		input.KeepRecentConversations = 0
	}
	if input.KeepRecentDays < 0 {
		input.KeepRecentDays = 0
	}

	byOwnerConversations := map[string][]store.Conversation{}
	for _, conversation := range conversations {
		ownerID := stringValue(conversation.Metadata["owner_id"], "unknown")
		if input.OwnerID != "" && ownerID != input.OwnerID {
			continue
		}
		byOwnerConversations[ownerID] = append(byOwnerConversations[ownerID], conversation)
	}

	now := time.Now().UTC()
	var cutoff time.Time
	if input.KeepRecentDays > 0 {
		cutoff = now.AddDate(0, 0, -input.KeepRecentDays)
	}

	ownerIDs := make([]string, 0, len(byOwnerConversations))
	for ownerID := range byOwnerConversations {
		ownerIDs = append(ownerIDs, ownerID)
	}
	sort.Strings(ownerIDs)

	preview := StorageCleanupPreview{
		GeneratedAt:             now,
		DryRun:                  true,
		OwnerID:                 input.OwnerID,
		KeepRecentConversations: input.KeepRecentConversations,
		KeepRecentDays:          input.KeepRecentDays,
		ByOwner:                 make([]StorageCleanupOwnerPlan, 0, len(ownerIDs)),
	}
	for _, ownerID := range ownerIDs {
		items := byOwnerConversations[ownerID]
		sort.SliceStable(items, func(i, j int) bool {
			return storageConversationTime(items[i]).After(storageConversationTime(items[j]))
		})

		ownerPlan := StorageCleanupOwnerPlan{OwnerID: ownerID}
		for index, conversation := range items {
			if input.KeepRecentConversations > 0 && index < input.KeepRecentConversations {
				continue
			}
			if !cutoff.IsZero() && storageConversationTime(conversation).After(cutoff) {
				continue
			}

			messages, err := s.store.ListMessages(ctx, conversation.ID)
			if err != nil {
				return StorageCleanupPreview{}, err
			}
			bytes := storageConversationEstimatedBytes(conversation, messages)
			ownerPlan.CandidateConversations++
			ownerPlan.CandidateMessages += len(messages)
			ownerPlan.EstimatedReclaimableBytes += bytes
			preview.CandidateConversations++
			preview.CandidateMessages += len(messages)
			preview.EstimatedReclaimableBytes += bytes
		}
		preview.ByOwner = append(preview.ByOwner, ownerPlan)
	}
	return preview, nil
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

func storageConversationTime(conversation store.Conversation) time.Time {
	if !conversation.LastMessageAt.IsZero() {
		return conversation.LastMessageAt
	}
	if !conversation.UpdatedAt.IsZero() {
		return conversation.UpdatedAt
	}
	return conversation.CreatedAt
}

func storageConversationEstimatedBytes(conversation store.Conversation, messages []store.Message) int64 {
	bytes := estimatedJSONBytes(conversation.Metadata)
	bytes += int64(len(conversation.Title) + len(conversation.LastMessagePreview) + len(conversation.LastUserText))
	for _, message := range messages {
		bytes += int64(len(message.Content) + len(message.Role) + len(message.Status))
		bytes += estimatedJSONBytes(message.Metadata)
	}
	return bytes
}

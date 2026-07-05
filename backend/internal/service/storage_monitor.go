package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	UserID                    string `json:"user_id"`
	EstimatedBytes            int64  `json:"estimated_bytes"`
	StorageQuotaBytes         int64  `json:"storage_quota_bytes"`
	StorageQuotaDefaultBytes  int64  `json:"storage_quota_default_bytes"`
	StorageQuotaOverrideBytes *int64 `json:"storage_quota_override_bytes,omitempty"`
	StorageOverQuota          bool   `json:"storage_over_quota"`
	ConversationCount         int    `json:"conversation_count"`
	MessageCount              int    `json:"message_count"`
	ImageCount                int    `json:"image_count"`
	ImageBytes                int64  `json:"image_bytes"`
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
	DeletedConversations      int                       `json:"deleted_conversations,omitempty"`
	DeletedMessages           int                       `json:"deleted_messages,omitempty"`
	ByOwner                   []StorageCleanupOwnerPlan `json:"by_owner"`
}

type StorageCleanupOwnerPlan struct {
	OwnerID                   string `json:"owner_id"`
	CandidateConversations    int    `json:"candidate_conversations"`
	CandidateMessages         int    `json:"candidate_messages"`
	EstimatedReclaimableBytes int64  `json:"estimated_reclaimable_bytes"`
}

type StorageVacuumResult struct {
	GeneratedAt time.Time     `json:"generated_at"`
	DryRun      bool          `json:"dry_run"`
	Before      DatabaseInfo  `json:"before"`
	After       *DatabaseInfo `json:"after,omitempty"`
}

type storageCleanupCandidate struct {
	ConversationID string
	OwnerID        string
	MessageCount   int
	EstimatedBytes int64
}

type StorageOrphanImagesPreview struct {
	GeneratedAt  time.Time            `json:"generated_at"`
	DryRun       bool                 `json:"dry_run"`
	Root         string               `json:"root"`
	FileCount    int                  `json:"file_count"`
	Bytes        int64                `json:"bytes"`
	DeletedCount int                  `json:"deleted_count,omitempty"`
	DeletedBytes int64                `json:"deleted_bytes,omitempty"`
	Items        []StorageOrphanImage `json:"items"`
}

type StorageOrphanImage struct {
	Filename string    `json:"filename"`
	Path     string    `json:"path"`
	Bytes    int64     `json:"bytes"`
	ModTime  time.Time `json:"mod_time"`
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
	quotas, err := s.store.ListStorageUserQuotas(ctx)
	if err != nil {
		return nil, err
	}
	for _, quota := range quotas {
		usage := byUser[quota.OwnerID]
		if usage == nil {
			usage = &UserStorageUsage{UserID: quota.OwnerID}
			byUser[quota.OwnerID] = usage
		}
		quotaBytes := quota.QuotaBytes
		usage.StorageQuotaOverrideBytes = &quotaBytes
	}
	items := make([]UserStorageUsage, 0, len(byUser))
	for _, item := range byUser {
		item.StorageQuotaDefaultBytes = s.cfg.StorageDefaultQuotaBytes
		item.StorageQuotaBytes = s.effectiveQuotaBytes(*item)
		item.StorageOverQuota = item.StorageQuotaBytes > 0 && item.EstimatedBytes > item.StorageQuotaBytes
		items = append(items, *item)
	}
	return items, nil
}

func (s *StorageMonitorService) SetUserQuota(ctx context.Context, ownerID string, quotaBytes int64) (store.StorageUserQuota, error) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return store.StorageUserQuota{}, errors.New("owner_id is required")
	}
	if quotaBytes < 0 {
		return store.StorageUserQuota{}, errors.New("quota_bytes must be non-negative")
	}
	return s.store.SetStorageUserQuota(ctx, ownerID, quotaBytes)
}

func (s *StorageMonitorService) DeleteUserQuota(ctx context.Context, ownerID string) error {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return errors.New("owner_id is required")
	}
	return s.store.DeleteStorageUserQuota(ctx, ownerID)
}

func (s *StorageMonitorService) effectiveQuotaBytes(usage UserStorageUsage) int64 {
	if usage.StorageQuotaOverrideBytes != nil {
		return *usage.StorageQuotaOverrideBytes
	}
	return s.cfg.StorageDefaultQuotaBytes
}

func (s *StorageMonitorService) CleanupPreview(ctx context.Context, input StorageCleanupPreviewInput) (StorageCleanupPreview, error) {
	preview, _, err := s.cleanupPlan(ctx, input)
	return preview, err
}

func (s *StorageMonitorService) DeleteCleanupCandidates(ctx context.Context, input StorageCleanupPreviewInput) (StorageCleanupPreview, error) {
	preview, candidates, err := s.cleanupPlan(ctx, input)
	if err != nil {
		return StorageCleanupPreview{}, err
	}
	preview.DryRun = false
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ConversationID)
	}
	result, err := s.store.DeleteConversations(ctx, ids)
	if err != nil {
		return StorageCleanupPreview{}, err
	}
	preview.DeletedConversations = result.DeletedConversations
	preview.DeletedMessages = result.DeletedMessages
	return preview, nil
}

func (s *StorageMonitorService) Vacuum(ctx context.Context, dryRun bool) (StorageVacuumResult, error) {
	result := StorageVacuumResult{
		GeneratedAt: time.Now().UTC(),
		DryRun:      dryRun,
		Before:      storageDatabaseInfo(s.cfg),
	}
	if dryRun {
		return result, nil
	}
	if s.cfg.DatabaseDriver != "sqlite" {
		return StorageVacuumResult{}, errors.New("storage vacuum currently supports sqlite only")
	}
	if err := s.store.Vacuum(ctx); err != nil {
		return StorageVacuumResult{}, err
	}
	after := storageDatabaseInfo(s.cfg)
	result.After = &after
	return result, nil
}

func (s *StorageMonitorService) cleanupPlan(ctx context.Context, input StorageCleanupPreviewInput) (StorageCleanupPreview, []storageCleanupCandidate, error) {
	conversations, err := s.store.ListConversations(ctx)
	if err != nil {
		return StorageCleanupPreview{}, nil, err
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
		if isActiveStorageConversation(conversation) {
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
	candidates := make([]storageCleanupCandidate, 0)
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
				return StorageCleanupPreview{}, nil, err
			}
			bytes := storageConversationEstimatedBytes(conversation, messages)
			candidates = append(candidates, storageCleanupCandidate{
				ConversationID: conversation.ID,
				OwnerID:        ownerID,
				MessageCount:   len(messages),
				EstimatedBytes: bytes,
			})
			ownerPlan.CandidateConversations++
			ownerPlan.CandidateMessages += len(messages)
			ownerPlan.EstimatedReclaimableBytes += bytes
			preview.CandidateConversations++
			preview.CandidateMessages += len(messages)
			preview.EstimatedReclaimableBytes += bytes
		}
		preview.ByOwner = append(preview.ByOwner, ownerPlan)
	}
	return preview, candidates, nil
}

func (s *StorageMonitorService) OrphanImagesPreview(ctx context.Context) (StorageOrphanImagesPreview, error) {
	root := filepath.Join(s.cfg.DataDir, "uploads", "imgs")
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return StorageOrphanImagesPreview{}, err
	}
	preview := StorageOrphanImagesPreview{
		GeneratedAt: time.Now().UTC(),
		DryRun:      true,
		Root:        absRoot,
		Items:       []StorageOrphanImage{},
	}

	knownImages, err := s.store.ListUploadedImages(ctx)
	if err != nil {
		return StorageOrphanImagesPreview{}, err
	}
	knownFilenames := make(map[string]struct{}, len(knownImages))
	for _, image := range knownImages {
		knownFilenames[image.Filename] = struct{}{}
	}

	if _, err := os.Stat(absRoot); err != nil {
		if os.IsNotExist(err) {
			return preview, nil
		}
		return StorageOrphanImagesPreview{}, err
	}
	err = filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, err error) error {
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
		relative, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		if relative == "." || relative == ".." || filepath.Dir(relative) != "." {
			return nil
		}
		filename := filepath.Base(relative)
		if _, ok := knownFilenames[filename]; ok {
			return nil
		}
		stat, err := entry.Info()
		if err != nil {
			return err
		}
		preview.FileCount++
		preview.Bytes += stat.Size()
		preview.Items = append(preview.Items, StorageOrphanImage{
			Filename: filename,
			Path:     path,
			Bytes:    stat.Size(),
			ModTime:  stat.ModTime().UTC(),
		})
		return nil
	})
	if err != nil {
		return StorageOrphanImagesPreview{}, err
	}
	sort.SliceStable(preview.Items, func(i, j int) bool {
		return preview.Items[i].Filename < preview.Items[j].Filename
	})
	return preview, nil
}

func (s *StorageMonitorService) DeleteOrphanImages(ctx context.Context) (StorageOrphanImagesPreview, error) {
	preview, err := s.OrphanImagesPreview(ctx)
	if err != nil {
		return StorageOrphanImagesPreview{}, err
	}
	preview.DryRun = false
	for _, item := range preview.Items {
		select {
		case <-ctx.Done():
			return StorageOrphanImagesPreview{}, ctx.Err()
		default:
		}
		if !isDirectChildPath(preview.Root, item.Path) {
			continue
		}
		if err := os.Remove(item.Path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return StorageOrphanImagesPreview{}, err
		}
		preview.DeletedCount++
		preview.DeletedBytes += item.Bytes
	}
	return preview, nil
}

func isDirectChildPath(root string, path string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return relative != "." && relative != ".." && filepath.Dir(relative) == "."
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

func isActiveStorageConversation(conversation store.Conversation) bool {
	status := stringValue(conversation.Metadata["realtime_status"], "")
	return status == "waiting" || status == "streaming"
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

package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
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

type StorageQuotaPruneResult struct {
	GeneratedAt  time.Time               `json:"generated_at"`
	CheckedUsers int                     `json:"checked_users"`
	OverQuota    int                     `json:"over_quota"`
	PrunedUsers  int                     `json:"pruned_users"`
	Results      []StorageCleanupPreview `json:"results"`
}

type StorageCleanupPreview struct {
	GeneratedAt               time.Time                 `json:"generated_at"`
	DryRun                    bool                      `json:"dry_run"`
	OwnerID                   string                    `json:"owner_id,omitempty"`
	KeepRecentConversations   int                       `json:"keep_recent_conversations"`
	KeepRecentDays            int                       `json:"keep_recent_days"`
	CandidateConversations    int                       `json:"candidate_conversations"`
	CandidateMessages         int                       `json:"candidate_messages"`
	CandidateImages           int                       `json:"candidate_images"`
	CandidateImageBytes       int64                     `json:"candidate_image_bytes"`
	EstimatedReclaimableBytes int64                     `json:"estimated_reclaimable_bytes"`
	DeletedConversations      int                       `json:"deleted_conversations,omitempty"`
	DeletedMessages           int                       `json:"deleted_messages,omitempty"`
	DeletedImages             int                       `json:"deleted_images,omitempty"`
	DeletedImageBytes         int64                     `json:"deleted_image_bytes,omitempty"`
	ImageDeleteFailures       int                       `json:"image_delete_failures,omitempty"`
	ByOwner                   []StorageCleanupOwnerPlan `json:"by_owner"`
}

type StorageCleanupOwnerPlan struct {
	OwnerID                   string `json:"owner_id"`
	CandidateConversations    int    `json:"candidate_conversations"`
	CandidateMessages         int    `json:"candidate_messages"`
	CandidateImages           int    `json:"candidate_images"`
	CandidateImageBytes       int64  `json:"candidate_image_bytes"`
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
	ImageFilenames []string
}

type storageCleanupImagePlan struct {
	Images []store.UploadedImage
}

type storageCleanupImageResult struct {
	DeletedImages     int
	DeletedImageBytes int64
	DeleteFailures    int
}

type StorageFileDeletionRetryResult struct {
	GeneratedAt time.Time `json:"generated_at"`
	Scanned     int       `json:"scanned"`
	Deleted     int       `json:"deleted"`
	Failed      int       `json:"failed"`
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

func (s *StorageMonitorService) PruneOverQuotaUsers(ctx context.Context, input StorageCleanupPreviewInput) (StorageQuotaPruneResult, error) {
	users, err := s.Users(ctx)
	if err != nil {
		return StorageQuotaPruneResult{}, err
	}
	sort.SliceStable(users, func(i, j int) bool {
		return users[i].UserID < users[j].UserID
	})
	result := StorageQuotaPruneResult{
		GeneratedAt:  time.Now().UTC(),
		CheckedUsers: len(users),
		Results:      []StorageCleanupPreview{},
	}
	for _, user := range users {
		if !user.StorageOverQuota {
			continue
		}
		select {
		case <-ctx.Done():
			return StorageQuotaPruneResult{}, ctx.Err()
		default:
		}
		result.OverQuota++
		pruneInput := input
		pruneInput.OwnerID = user.UserID
		cleanup, err := s.DeleteCleanupCandidates(ctx, pruneInput)
		if err != nil {
			return StorageQuotaPruneResult{}, err
		}
		if cleanup.DeletedConversations > 0 || cleanup.DeletedImages > 0 || cleanup.ImageDeleteFailures > 0 {
			result.PrunedUsers++
		}
		result.Results = append(result.Results, cleanup)
	}
	return result, nil
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
	imagePlan, err := s.cleanupImagePlan(ctx, candidates)
	if err != nil {
		return StorageCleanupPreview{}, err
	}
	result, err := s.store.DeleteConversations(ctx, ids)
	if err != nil {
		return StorageCleanupPreview{}, err
	}
	imageResult, err := s.deleteCleanupImages(ctx, imagePlan)
	if err != nil {
		return StorageCleanupPreview{}, err
	}
	preview.DeletedConversations = result.DeletedConversations
	preview.DeletedMessages = result.DeletedMessages
	preview.DeletedImages = imageResult.DeletedImages
	preview.DeletedImageBytes = imageResult.DeletedImageBytes
	preview.ImageDeleteFailures = imageResult.DeleteFailures
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

func (s *StorageMonitorService) Checkpoint(ctx context.Context) error {
	if s.cfg.DatabaseDriver != "sqlite" {
		return errors.New("storage checkpoint currently supports sqlite only")
	}
	return s.store.Checkpoint(ctx)
}

func (s *StorageMonitorService) cleanupPlan(ctx context.Context, input StorageCleanupPreviewInput) (StorageCleanupPreview, []storageCleanupCandidate, error) {
	conversations, err := s.store.ListConversations(ctx)
	if err != nil {
		return StorageCleanupPreview{}, nil, err
	}
	uploadedImages, err := s.store.ListUploadedImages(ctx)
	if err != nil {
		return StorageCleanupPreview{}, nil, err
	}
	imageByFilename := map[string]store.UploadedImage{}
	for _, image := range uploadedImages {
		imageByFilename[image.Filename] = image
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
			imageRefs := storageImageReferences(conversation, messages)
			imageCount, imageBytes := cleanupImageEstimate(imageRefs, imageByFilename)
			bytes := storageConversationEstimatedBytes(conversation, messages) + imageBytes
			candidates = append(candidates, storageCleanupCandidate{
				ConversationID: conversation.ID,
				OwnerID:        ownerID,
				MessageCount:   len(messages),
				EstimatedBytes: bytes,
				ImageFilenames: mapKeys(imageRefs),
			})
			ownerPlan.CandidateConversations++
			ownerPlan.CandidateMessages += len(messages)
			ownerPlan.CandidateImages += imageCount
			ownerPlan.CandidateImageBytes += imageBytes
			ownerPlan.EstimatedReclaimableBytes += bytes
			preview.CandidateConversations++
			preview.CandidateMessages += len(messages)
			preview.CandidateImages += imageCount
			preview.CandidateImageBytes += imageBytes
			preview.EstimatedReclaimableBytes += bytes
		}
		preview.ByOwner = append(preview.ByOwner, ownerPlan)
	}
	return preview, candidates, nil
}

func (s *StorageMonitorService) cleanupImagePlan(ctx context.Context, candidates []storageCleanupCandidate) (storageCleanupImagePlan, error) {
	if len(candidates) == 0 {
		return storageCleanupImagePlan{}, nil
	}
	candidateIDs := map[string]struct{}{}
	candidateOwners := map[string]struct{}{}
	candidateFilenames := map[string]struct{}{}
	for _, candidate := range candidates {
		candidateIDs[candidate.ConversationID] = struct{}{}
		candidateOwners[candidate.OwnerID] = struct{}{}
		for _, filename := range candidate.ImageFilenames {
			candidateFilenames[filename] = struct{}{}
		}
	}
	if len(candidateFilenames) == 0 {
		return storageCleanupImagePlan{}, nil
	}

	stillReferenced, err := s.referencedImagesOutsideCandidates(ctx, candidateIDs)
	if err != nil {
		return storageCleanupImagePlan{}, err
	}
	uploadedImages, err := s.store.ListUploadedImages(ctx)
	if err != nil {
		return storageCleanupImagePlan{}, err
	}
	plan := storageCleanupImagePlan{Images: []store.UploadedImage{}}
	for _, image := range uploadedImages {
		if _, ok := candidateFilenames[image.Filename]; !ok {
			continue
		}
		if _, ok := stillReferenced[image.Filename]; ok {
			continue
		}
		if _, ok := candidateOwners[image.OwnerID]; !ok {
			continue
		}
		plan.Images = append(plan.Images, image)
	}
	sort.SliceStable(plan.Images, func(i, j int) bool {
		return plan.Images[i].Filename < plan.Images[j].Filename
	})
	return plan, nil
}

func (s *StorageMonitorService) referencedImagesOutsideCandidates(ctx context.Context, candidateIDs map[string]struct{}) (map[string]struct{}, error) {
	conversations, err := s.store.ListConversations(ctx)
	if err != nil {
		return nil, err
	}
	referenced := map[string]struct{}{}
	for _, conversation := range conversations {
		if _, ok := candidateIDs[conversation.ID]; ok {
			continue
		}
		messages, err := s.store.ListMessages(ctx, conversation.ID)
		if err != nil {
			return nil, err
		}
		for filename := range storageImageReferences(conversation, messages) {
			referenced[filename] = struct{}{}
		}
	}
	return referenced, nil
}

func (s *StorageMonitorService) deleteCleanupImages(ctx context.Context, plan storageCleanupImagePlan) (storageCleanupImageResult, error) {
	if len(plan.Images) == 0 {
		return storageCleanupImageResult{}, nil
	}
	root := filepath.Join(s.cfg.DataDir, "uploads", "imgs")
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return storageCleanupImageResult{}, err
	}
	result := storageCleanupImageResult{}
	deletedFilenames := make([]string, 0, len(plan.Images))
	for _, image := range plan.Images {
		select {
		case <-ctx.Done():
			return storageCleanupImageResult{}, ctx.Err()
		default:
		}
		path := filepath.Join(absRoot, image.Filename)
		if !isDirectChildPath(absRoot, path) {
			continue
		}
		if err := os.Remove(path); err != nil {
			if !os.IsNotExist(err) {
				result.DeleteFailures++
				if recordErr := s.recordFileDeletionFailure(ctx, path, image.Filename, image.OwnerID, image.Bytes, err); recordErr != nil {
					return storageCleanupImageResult{}, recordErr
				}
				continue
			}
		}
		deletedFilenames = append(deletedFilenames, image.Filename)
		result.DeletedImageBytes += image.Bytes
	}
	deleteResult, err := s.store.DeleteUploadedImagesByFilenames(ctx, deletedFilenames)
	if err != nil {
		return storageCleanupImageResult{}, err
	}
	result.DeletedImages = deleteResult.DeletedImages
	return result, nil
}

func (s *StorageMonitorService) RetryFileDeletionFailures(ctx context.Context, limit int) (StorageFileDeletionRetryResult, error) {
	items, err := s.store.ListStorageFileDeletionFailures(ctx, limit)
	if err != nil {
		return StorageFileDeletionRetryResult{}, err
	}
	result := StorageFileDeletionRetryResult{
		GeneratedAt: time.Now().UTC(),
		Scanned:     len(items),
	}
	root := filepath.Join(s.cfg.DataDir, "uploads", "imgs")
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return StorageFileDeletionRetryResult{}, err
	}
	deletedPaths := make([]string, 0, len(items))
	deletedFilenames := make([]string, 0, len(items))
	for _, item := range items {
		select {
		case <-ctx.Done():
			return StorageFileDeletionRetryResult{}, ctx.Err()
		default:
		}
		if !isDirectChildPath(absRoot, item.Path) {
			result.Failed++
			if _, err := s.store.UpsertStorageFileDeletionFailure(ctx, store.UpsertStorageFileDeletionFailureInput{
				Path:      item.Path,
				Filename:  item.Filename,
				OwnerID:   item.OwnerID,
				Bytes:     item.Bytes,
				LastError: "file path is outside uploads/imgs",
			}); err != nil {
				return StorageFileDeletionRetryResult{}, err
			}
			continue
		}
		if err := os.Remove(item.Path); err != nil {
			if os.IsNotExist(err) {
				deletedPaths = append(deletedPaths, item.Path)
				deletedFilenames = append(deletedFilenames, item.Filename)
				result.Deleted++
				continue
			}
			result.Failed++
			if _, err := s.store.UpsertStorageFileDeletionFailure(ctx, store.UpsertStorageFileDeletionFailureInput{
				Path:      item.Path,
				Filename:  item.Filename,
				OwnerID:   item.OwnerID,
				Bytes:     item.Bytes,
				LastError: err.Error(),
			}); err != nil {
				return StorageFileDeletionRetryResult{}, err
			}
			continue
		}
		deletedPaths = append(deletedPaths, item.Path)
		deletedFilenames = append(deletedFilenames, item.Filename)
		result.Deleted++
	}
	if _, err := s.store.DeleteUploadedImagesByFilenames(ctx, deletedFilenames); err != nil {
		return StorageFileDeletionRetryResult{}, err
	}
	if err := s.store.DeleteStorageFileDeletionFailures(ctx, deletedPaths); err != nil {
		return StorageFileDeletionRetryResult{}, err
	}
	return result, nil
}

func (s *StorageMonitorService) recordFileDeletionFailure(ctx context.Context, path string, filename string, ownerID string, bytes int64, cause error) error {
	_, err := s.store.UpsertStorageFileDeletionFailure(ctx, store.UpsertStorageFileDeletionFailureInput{
		Path:      path,
		Filename:  filename,
		OwnerID:   ownerID,
		Bytes:     bytes,
		LastError: cause.Error(),
	})
	return err
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
			if recordErr := s.recordFileDeletionFailure(ctx, item.Path, item.Filename, "", item.Bytes, err); recordErr != nil {
				return StorageOrphanImagesPreview{}, recordErr
			}
			continue
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

var uploadImageURLPattern = regexp.MustCompile(`/api/uploads/imgs/([A-Za-z0-9._-]+)`)

func storageImageReferences(conversation store.Conversation, messages []store.Message) map[string]struct{} {
	references := map[string]struct{}{}
	addUploadImageReferences(references, conversation.Title)
	addUploadImageReferences(references, conversation.LastUserText)
	addUploadImageReferences(references, conversation.LastMessagePreview)
	addUploadImageReferencesFromJSON(references, conversation.Metadata)
	for _, message := range messages {
		addUploadImageReferences(references, message.Content)
		addUploadImageReferencesFromJSON(references, message.Metadata)
	}
	return references
}

func addUploadImageReferencesFromJSON(references map[string]struct{}, value any) {
	if value == nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	addUploadImageReferences(references, string(data))
}

func addUploadImageReferences(references map[string]struct{}, value string) {
	for _, match := range uploadImageURLPattern.FindAllStringSubmatch(value, -1) {
		if len(match) < 2 {
			continue
		}
		filename := strings.TrimSpace(match[1])
		if filename == "" || filename == "." || filename == ".." || filepath.Base(filename) != filename {
			continue
		}
		references[filename] = struct{}{}
	}
}

func cleanupImageEstimate(references map[string]struct{}, imageByFilename map[string]store.UploadedImage) (int, int64) {
	var count int
	var bytes int64
	for filename := range references {
		image, ok := imageByFilename[filename]
		if !ok {
			continue
		}
		count++
		bytes += image.Bytes
	}
	return count, bytes
}

func mapKeys(items map[string]struct{}) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

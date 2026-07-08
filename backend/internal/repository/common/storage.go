package common

import "time"

type UploadedImage struct {
	ID               string    `json:"id"`
	OwnerID          string    `json:"owner_id"`
	Filename         string    `json:"filename"`
	OriginalFilename string    `json:"original_filename,omitempty"`
	ContentType      string    `json:"content_type"`
	Bytes            int64     `json:"bytes"`
	URL              string    `json:"url"`
	CreatedAt        time.Time `json:"created_at"`
}

type MediaAsset struct {
	ID                string    `json:"id"`
	OwnerID           string    `json:"owner_id"`
	FileID            string    `json:"file_id"`
	Path              string    `json:"path"`
	MediaType         string    `json:"media_type"`
	Bytes             int64     `json:"bytes"`
	SHA256            string    `json:"sha256"`
	Width             int       `json:"width"`
	Height            int       `json:"height"`
	SourceKind        string    `json:"source_kind"`
	OriginalName      string    `json:"original_name,omitempty"`
	OriginalMediaType string    `json:"original_media_type,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

type MediaAssetRef struct {
	ID             string    `json:"id"`
	AssetID        string    `json:"asset_id"`
	FileID         string    `json:"file_id"`
	OwnerID        string    `json:"owner_id"`
	RequestID      string    `json:"request_id"`
	ConversationID string    `json:"conversation_id"`
	MessageID      string    `json:"message_id"`
	InputPartIndex int       `json:"input_part_index"`
	CreatedAt      time.Time `json:"created_at"`
}

type StorageUserQuota struct {
	OwnerID    string    `json:"owner_id"`
	QuotaBytes int64     `json:"quota_bytes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type StorageFileDeletionFailure struct {
	Path      string    `json:"path"`
	Filename  string    `json:"filename,omitempty"`
	OwnerID   string    `json:"owner_id,omitempty"`
	Bytes     int64     `json:"bytes"`
	LastError string    `json:"last_error"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

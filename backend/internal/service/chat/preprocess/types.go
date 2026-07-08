package preprocess

import "github.com/zyf2007/ChatAPI/internal/protocol"

type PreparedImage struct {
	FileID            string `json:"file_id"`
	Path              string `json:"path"`
	MediaType         string `json:"media_type"`
	Bytes             int64  `json:"bytes"`
	SHA256            string `json:"sha256"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	SourceKind        string `json:"source_kind"`
	OriginalName      string `json:"original_name,omitempty"`
	OriginalMediaType string `json:"original_media_type,omitempty"`
	InputPartIndex    int    `json:"input_part_index"`
}

type PreparedRequest struct {
	Request        protocol.TurnRequest `json:"request"`
	PreparedImages []PreparedImage      `json:"prepared_images,omitempty"`
}

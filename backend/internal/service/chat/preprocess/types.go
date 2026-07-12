package preprocess

import (
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/protocol"
)

type PreparedRequest struct {
	Request        protocol.TurnRequest `json:"request"`
	PreparedImages []media.DraftAsset   `json:"prepared_images,omitempty"`
}

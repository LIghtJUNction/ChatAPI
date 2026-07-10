package workspace

import (
	"testing"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
)

func TestTimelineItemProjectsGeneratedImageURL(t *testing.T) {
	event := common.ConversationEvent{
		ID: "evt_image", Type: "builtin_tool",
		MediaAssets: []common.EventMediaAssetRef{{URL: "/api/media/assets/file_image", MediaType: "image/avif"}},
	}
	item := TimelineItemFromRaw(timelinesvc.ItemFromConversationEvent(event))
	if len(item.ContentParts) != 1 || item.ContentParts[0].Src != "/api/media/assets/file_image" || item.ContentParts[0].MediaType != "image/avif" {
		t.Fatalf("unexpected generated image projection: %#v", item)
	}
}

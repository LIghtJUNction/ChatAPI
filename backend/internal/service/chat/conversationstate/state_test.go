package conversationstate

import (
	"testing"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func TestFromConversationPreservesDraftWhitespace(t *testing.T) {
	conversation := common.Conversation{Metadata: map[string]any{
		"request_id":          "  req_1  ",
		"realtime_draft_text": "now\n",
	}}

	runtime := FromConversation(conversation)
	if runtime.RequestID != "req_1" {
		t.Fatalf("identifier was not normalized: %q", runtime.RequestID)
	}
	if runtime.DraftText != "now\n" {
		t.Fatalf("draft whitespace was not preserved: %q", runtime.DraftText)
	}
}

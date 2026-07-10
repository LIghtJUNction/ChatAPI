package protocolruntime

import (
	"strings"

	"github.com/zyf2007/ChatAPI/internal/protocol"
)

type BuiltinToolSpec struct {
	Kind          string
	Title         string
	RequiresQuery bool
	RequiresAsset bool
}

func LookupBuiltinToolSpec(kind string) (BuiltinToolSpec, bool) {
	switch strings.TrimSpace(kind) {
	case "web_search":
		return BuiltinToolSpec{
			Kind:          "web_search",
			Title:         "Web Search",
			RequiresQuery: true,
		}, true
	case "image_generation":
		return BuiltinToolSpec{
			Kind:          "image_generation",
			Title:         "Image Generation",
			RequiresAsset: true,
		}, true
	default:
		return BuiltinToolSpec{}, false
	}
}

func RequestSupportsBuiltinTool(request protocol.TurnRequest, kind string) bool {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return false
	}
	for _, item := range request.BuiltinTools {
		if strings.TrimSpace(item.Kind) == kind {
			return true
		}
	}
	return false
}

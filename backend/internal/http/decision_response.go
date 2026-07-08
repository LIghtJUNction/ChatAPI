package httpapi

import (
	"net/http"

	"github.com/zyf2007/ChatAPI/internal/http/httpx"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/decision"
)

func writeDecisionError(w http.ResponseWriter, result decision.Result) {
	status := result.StatusCode
	if status <= 0 {
		status = http.StatusForbidden
	}
	httpx.WriteJSON(w, status, map[string]any{
		"ok":         false,
		"error":      result.Message,
		"error_code": result.ErrorCode,
		"subject":    result.Subject,
		"reason":     result.Reason,
	})
}

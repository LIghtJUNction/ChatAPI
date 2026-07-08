package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/decision"
)

func WriteDecisionError(w http.ResponseWriter, result decision.Result) {
	status := result.StatusCode
	if status <= 0 {
		status = http.StatusForbidden
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":         false,
		"error":      result.Message,
		"error_code": result.ErrorCode,
		"subject":    result.Subject,
		"reason":     result.Reason,
	})
}

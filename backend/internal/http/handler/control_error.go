package handler

import (
	"errors"
	"net/http"

	"github.com/zyf2007/ChatAPI/internal/http/httpx"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
)

func writeControlError(w http.ResponseWriter, err error) bool {
	var controlErr *controlsvc.Error
	if !errors.As(err, &controlErr) {
		return false
	}
	httpx.WriteJSON(w, httpStatusForControlError(controlErr), map[string]any{
		"error": map[string]any{
			"code":    controlErr.Code,
			"message": controlErr.Message,
		},
	})
	return true
}

func httpStatusForControlError(err *controlsvc.Error) int {
	if err == nil {
		return http.StatusInternalServerError
	}
	switch err.Kind {
	case controlsvc.ErrorKindInvalid:
		return http.StatusBadRequest
	case controlsvc.ErrorKindForbidden:
		return http.StatusForbidden
	case controlsvc.ErrorKindNotFound:
		return http.StatusNotFound
	case controlsvc.ErrorKindConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

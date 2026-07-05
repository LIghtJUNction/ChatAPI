package middleware

import (
	"net/http"

	"github.com/zyf/chatapi/internal/service"
)

type auditResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *auditResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func AuditAppAPIRequests(auditService *service.AppAPIKeyService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := &auditResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(recorder, r)

			principal, ok := AppAPIPrincipalFromContext(r.Context())
			if !ok {
				return
			}

			errorCode := ""
			switch recorder.statusCode {
			case http.StatusUnauthorized:
				errorCode = "unauthorized"
			case http.StatusForbidden:
				errorCode = "forbidden"
			case http.StatusConflict:
				errorCode = "conflict"
			case http.StatusNotFound:
				errorCode = "not_found"
			case http.StatusTooManyRequests:
				errorCode = "rate_limited"
			}

			auditService.RecordAudit(r.Context(), principal, r.URL.Path, recorder.statusCode, errorCode)
		})
	}
}

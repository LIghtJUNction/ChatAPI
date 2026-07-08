package httpx

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

var filteredRequestHeaders = map[string]struct{}{
	"Authorization":       {},
	"Cookie":              {},
	"Proxy-Authorization": {},
	"Set-Cookie":          {},
}

func CaptureRequestMeta(r *http.Request) store.Request {
	if r == nil {
		return store.Request{}
	}
	return store.Request{
		RequestMethod:  strings.TrimSpace(r.Method),
		RequestPath:    requestPathOnly(r),
		RequestQuery:   cloneValues(r.URL.Query()),
		RequestHeaders: sanitizeHeaders(r.Header),
	}
}

func requestPathOnly(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return strings.TrimSpace(r.URL.Path)
}

func cloneValues(values url.Values) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(values))
	for key, items := range values {
		cloned[key] = append([]string(nil), items...)
	}
	return cloned
}

func sanitizeHeaders(header http.Header) map[string][]string {
	if len(header) == 0 {
		return nil
	}
	sanitized := make(map[string][]string)
	for key, values := range header {
		canonical := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if canonical == "" {
			continue
		}
		if _, blocked := filteredRequestHeaders[canonical]; blocked {
			continue
		}
		sanitized[canonical] = append([]string(nil), values...)
	}
	if len(sanitized) == 0 {
		return nil
	}
	return sanitized
}

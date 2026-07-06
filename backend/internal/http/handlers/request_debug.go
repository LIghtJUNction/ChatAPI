package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

var filteredRequestHeaders = map[string]struct{}{
	"Authorization":       {},
	"Cookie":              {},
	"X-ChatAPI-App-Key":   {},
	"Proxy-Authorization": {},
	"Set-Cookie":          {},
}

func captureRequestMeta(r *http.Request) store.Request {
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
		copied := make([]string, 0, len(items))
		for _, item := range items {
			if strings.TrimSpace(item) == "" {
				copied = append(copied, "")
				continue
			}
			copied = append(copied, strings.TrimSpace(item))
		}
		cloned[key] = copied
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
		copied := make([]string, 0, len(values))
		for _, value := range values {
			copied = append(copied, value)
		}
		sanitized[canonical] = copied
	}
	if len(sanitized) == 0 {
		return nil
	}
	return sanitized
}

func buildReplayView(baseURL string, item store.Request) map[string]any {
	if strings.TrimSpace(item.RequestMethod) == "" && strings.TrimSpace(item.RequestPath) == "" && len(item.RequestBody) == 0 {
		return nil
	}
	return map[string]any{
		"method":  item.RequestMethod,
		"path":    item.RequestPath,
		"query":   item.RequestQuery,
		"headers": item.RequestHeaders,
		"body":    item.RequestBody,
		"curl":    buildReplayCurl(baseURL, item),
	}
}

func buildReplayCurl(baseURL string, item store.Request) string {
	method := strings.TrimSpace(item.RequestMethod)
	if method == "" {
		method = http.MethodPost
	}
	target := strings.TrimRight(strings.TrimSpace(baseURL), "/") + item.RequestPath
	if target == "" {
		target = item.RequestPath
	}
	if len(item.RequestQuery) > 0 {
		query := url.Values{}
		for key, values := range item.RequestQuery {
			for _, value := range values {
				query.Add(key, value)
			}
		}
		encoded := query.Encode()
		if encoded != "" {
			target += "?" + encoded
		}
	}
	parts := []string{"curl", "-X", shellQuote(method), shellQuote(target)}
	headerKeys := make([]string, 0, len(item.RequestHeaders))
	for key := range item.RequestHeaders {
		headerKeys = append(headerKeys, key)
	}
	sort.Strings(headerKeys)
	for _, key := range headerKeys {
		for _, value := range item.RequestHeaders[key] {
			parts = append(parts, "-H", shellQuote(key+": "+value))
		}
	}
	if len(item.RequestBody) > 0 {
		body, err := json.MarshalIndent(item.RequestBody, "", "  ")
		if err == nil {
			parts = append(parts, "--data-raw", shellQuote(string(body)))
		}
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	var buffer bytes.Buffer
	buffer.WriteByte('\'')
	for _, ch := range value {
		if ch == '\'' {
			buffer.WriteString("'\"'\"'")
			continue
		}
		buffer.WriteRune(ch)
	}
	buffer.WriteByte('\'')
	return buffer.String()
}

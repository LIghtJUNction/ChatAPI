package httpx

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func RequestBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}
	return (&url.URL{Scheme: scheme, Host: host}).String()
}

func BuildReplayCurl(baseURL string, item common.Request) string {
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
		if encoded := query.Encode(); encoded != "" {
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

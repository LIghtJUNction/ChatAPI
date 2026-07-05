package handlers

import (
	"net"
	"net/http"
	"strings"
)

func clientIP(r *http.Request) string {
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		value := strings.TrimSpace(r.Header.Get(header))
		if value == "" {
			continue
		}
		if header == "X-Forwarded-For" {
			parts := strings.Split(value, ",")
			if len(parts) > 0 {
				value = strings.TrimSpace(parts[0])
			}
		}
		if value != "" {
			return value
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

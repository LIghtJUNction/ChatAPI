package app

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func ExtractRequestAPIKey(r *http.Request) string {
	if r == nil {
		return ""
	}
	if value := strings.TrimSpace(r.Header.Get("X-ChatAPI-App-Key")); value != "" {
		return value
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	return ""
}

func RequestSourceIP(r *http.Request, trustedProxies []string) string {
	if r == nil {
		return ""
	}
	remoteIP := hostFromRemoteAddr(r.RemoteAddr)
	if !isTrustedProxy(remoteIP, trustedProxies) {
		return remoteIP
	}
	if value := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); value != "" {
		parts := strings.Split(value, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				return part
			}
		}
	}
	if value := strings.TrimSpace(r.Header.Get("X-Real-IP")); value != "" {
		return value
	}
	return remoteIP
}

func hostFromRemoteAddr(remoteAddr string) string {
	host := strings.TrimSpace(remoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	return host
}

func isTrustedProxy(remoteIP string, trustedProxies []string) bool {
	if len(trustedProxies) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(remoteIP))
	if err != nil {
		return false
	}
	for _, rawRule := range trustedProxies {
		rule := strings.TrimSpace(rawRule)
		if rule == "" {
			continue
		}
		if strings.Contains(rule, "/") {
			prefix, err := netip.ParsePrefix(rule)
			if err == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		allowedAddr, err := netip.ParseAddr(rule)
		if err == nil && allowedAddr == addr {
			return true
		}
	}
	return false
}

func (s *Service) AllowSourceIP(principal Principal, remoteAddr string) bool {
	if len(principal.AllowedSourceIPs) == 0 {
		return true
	}
	host := strings.TrimSpace(remoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, rawRule := range principal.AllowedSourceIPs {
		rule := strings.TrimSpace(rawRule)
		if rule == "" {
			continue
		}
		if strings.Contains(rule, "/") {
			prefix, err := netip.ParsePrefix(rule)
			if err == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		allowedAddr, err := netip.ParseAddr(rule)
		if err == nil && allowedAddr == addr {
			return true
		}
	}
	return false
}

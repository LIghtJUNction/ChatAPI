package urlsafety

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

const MaxNtfyURLLength = 2048

var restrictedNetworks = mustParseRestrictedNetworks([]string{
	"0.0.0.0/8",
	"10.0.0.0/8",
	"100.64.0.0/10",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"224.0.0.0/4",
	"::/128",
	"::1/128",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
})

type URLSafetyResult struct {
	OK        bool
	IsPrivate bool
	Reason    string
}

func ValidateNtfyURL(raw string, allowPrivate bool) URLSafetyResult {
	value := strings.TrimSpace(raw)
	if value == "" {
		return URLSafetyResult{OK: true}
	}
	if len(value) > MaxNtfyURLLength {
		return URLSafetyResult{Reason: "ntfy 地址过长，最多 2048 个字符"}
	}
	for _, ch := range value {
		if ch < 32 || ch == 127 {
			return URLSafetyResult{Reason: "ntfy 地址不能包含控制字符"}
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return URLSafetyResult{Reason: "ntfy 地址格式无效"}
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return URLSafetyResult{Reason: "ntfy 地址只支持 http 或 https"}
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return URLSafetyResult{Reason: "ntfy 地址必须包含主机名"}
	}
	normalizedHost := strings.TrimSuffix(strings.ToLower(host), ".")
	if normalizedHost == "localhost" || strings.HasSuffix(normalizedHost, ".localhost") {
		if allowPrivate {
			return URLSafetyResult{OK: true, IsPrivate: true}
		}
		return URLSafetyResult{IsPrivate: true, Reason: "ntfy 地址不能指向本机或内网地址"}
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	addresses, resolveErr := resolveHostAddresses(normalizedHost, port)
	if resolveErr != nil {
		return URLSafetyResult{Reason: resolveErr.Error()}
	}
	restricted := false
	for _, address := range addresses {
		if isRestrictedAddr(address) {
			restricted = true
			break
		}
	}
	if restricted && !allowPrivate {
		return URLSafetyResult{IsPrivate: true, Reason: "ntfy 地址不能指向本机或内网地址"}
	}
	return URLSafetyResult{OK: true, IsPrivate: restricted}
}

func resolveHostAddresses(host string, port string) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{literal.Unmap()}, nil
	}
	records, err := net.LookupIP(host)
	if err != nil {
		return nil, errors.New("ntfy 地址域名解析失败")
	}
	addresses := make([]netip.Addr, 0, len(records))
	seen := map[netip.Addr]struct{}{}
	for _, record := range records {
		addr, ok := netip.AddrFromSlice(record)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		if _, exists := seen[addr]; exists {
			continue
		}
		seen[addr] = struct{}{}
		addresses = append(addresses, addr)
	}
	if len(addresses) == 0 {
		return nil, errors.New("ntfy 地址域名没有可用的 IP 解析结果")
	}
	return addresses, nil
}

func isRestrictedAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.Unmap()
	for _, prefix := range restrictedNetworks {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func mustParseRestrictedNetworks(values []string) []netip.Prefix {
	items := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			panic(err)
		}
		items = append(items, prefix)
	}
	return items
}

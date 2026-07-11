package config

import (
	"os"
	"strings"
)

// SettingsEnvironment returns explicitly configured environment values that
// remain authoritative over database-managed runtime settings.
func (c Config) SettingsEnvironment(domain string) map[string]any {
	out := map[string]any{}
	set := func(env, key string, value any) {
		if c.settingsEnvironment[env] {
			out[key] = value
		}
	}
	switch domain {
	case "chat":
		set("CHATAPI_PENDING_TURN_TTL", "pending_turn_ttl", c.PendingTurnTTL.String())
	case "access":
		set("CHATAPI_ACCESS_RATE_LIMIT_REQUESTS", "global_rate_limit_requests", c.AccessRateLimitRequests)
		set("CHATAPI_ACCESS_RATE_LIMIT_WINDOW", "global_rate_limit_window", c.AccessRateLimitWindow.String())
	case "media":
		set("CHATAPI_MEDIA_PROCESS_ENABLED", "enabled", c.MediaProcessEnabled)
		set("CHATAPI_MEDIA_ALLOW_REMOTE_URL", "allow_remote_url", c.MediaAllowRemoteURL)
		set("CHATAPI_MEDIA_ALLOW_DATA_URL", "allow_data_url", c.MediaAllowDataURL)
		set("CHATAPI_MEDIA_ALLOW_BASE64", "allow_base64", c.MediaAllowBase64)
		set("CHATAPI_MEDIA_ALLOW_SVG", "allow_svg", c.MediaAllowSVG)
		set("CHATAPI_MEDIA_MAX_BYTES", "max_bytes", c.MediaMaxBytes)
		set("CHATAPI_MEDIA_MAX_IMAGES_PER_REQUEST", "max_images_per_request", c.MediaMaxImagesPerRequest)
		set("CHATAPI_MEDIA_AVIF_QUALITY", "avif_quality", c.MediaAVIFQuality)
	case "realtime":
		set("CHATAPI_REALTIME_MAX_CONNECTIONS", "max_connections_per_instance", c.RealtimeMaxConnections)
		set("CHATAPI_REALTIME_MAX_CONNECTIONS_PER_USER", "max_connections_per_user_per_instance", c.RealtimeMaxConnectionsPerUser)
	}
	return out
}

func detectSettingsEnvironment() map[string]bool {
	out := map[string]bool{}
	for _, name := range []string{"CHATAPI_ACCESS_RATE_LIMIT_REQUESTS", "CHATAPI_ACCESS_RATE_LIMIT_WINDOW", "CHATAPI_MEDIA_MAX_BYTES", "CHATAPI_MEDIA_MAX_IMAGES_PER_REQUEST", "CHATAPI_MEDIA_AVIF_QUALITY", "CHATAPI_PENDING_TURN_TTL", "CHATAPI_REALTIME_MAX_CONNECTIONS", "CHATAPI_REALTIME_MAX_CONNECTIONS_PER_USER"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			out[name] = true
		}
	}
	for _, name := range []string{"CHATAPI_MEDIA_PROCESS_ENABLED", "CHATAPI_MEDIA_ALLOW_REMOTE_URL", "CHATAPI_MEDIA_ALLOW_DATA_URL", "CHATAPI_MEDIA_ALLOW_BASE64", "CHATAPI_MEDIA_ALLOW_SVG"} {
		if validBoolSettingEnvironment(os.Getenv(name)) {
			out[name] = true
		}
	}
	return out
}

func validBoolSettingEnvironment(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on", "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

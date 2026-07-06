package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

type ConfigSystemHandler struct {
	Config  config.Config
	Service *service.SystemSettingsService
	Audit   *service.AuditService
}

func (h ConfigSystemHandler) Get(w http.ResponseWriter, r *http.Request) {
	if !isAdminRequest(r) {
		http.Error(w, "admin session required", http.StatusUnauthorized)
		return
	}
	settings, err := h.Service.Get(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeSystemSettingsResponse(w, settings)
}

func (h ConfigSystemHandler) Post(w http.ResponseWriter, r *http.Request) {
	if !isAdminRequest(r) {
		http.Error(w, "admin session required", http.StatusUnauthorized)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid system settings request", http.StatusBadRequest)
		return
	}
	settings, err := h.Service.Set(r.Context(), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if h.Audit != nil {
		h.Audit.Record(r.Context(), service.AuditEventInput{
			EventType:    "admin.config",
			ResourceType: "system_settings",
			Action:       "update",
			Outcome:      "success",
			IPAddress:    clientIP(r),
			UserAgent:    r.UserAgent(),
		})
	}
	writeSystemSettingsResponse(w, settings)
}

func writeSystemSettingsResponse(w http.ResponseWriter, settings service.SystemSettings) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                            true,
		"public_statistics":             settings.PublicStatistics,
		"title_enabled":                 settings.TitleEnabled,
		"title":                         settings.Title,
		"external_registration_enabled": settings.ExternalRegistrationEnabled,
		"email_verification_enabled":    settings.EmailVerificationEnabled,
		"email_provider":                settings.EmailProvider,
		"email_provider_options":        settings.EmailProviderOptions,
		"registration_email_domain_restriction_enabled": settings.RegistrationEmailDomainRestrictionEnabled,
		"registration_email_domains":                    settings.RegistrationEmailDomains,
		"ntfy_private_url_policy":                       settings.NtfyPrivateURLPolicy,
		"api_key_limit_per_user":                        settings.APIKeyLimitPerUser,
		"realtime_max_connections":                      settings.RealtimeMaxConnections,
		"realtime_max_connections_per_user":             settings.RealtimeMaxConnectionsPerUser,
		"realtime_queue_size":                           settings.RealtimeQueueSize,
		"image_max_single_bytes":                        settings.ImageMaxSingleBytes,
		"image_max_request_bytes":                       settings.ImageMaxRequestBytes,
		"image_max_total_bytes":                         settings.ImageMaxTotalBytes,
		"pending_max_per_user":                          settings.PendingMaxPerUser,
		"pending_max_age_hours":                         settings.PendingMaxAgeHours,
		"pending_max_output_chars":                      settings.PendingMaxOutputChars,
		"pending_auto_abort_message":                    settings.PendingAutoAbortMessage,
		"image_usage":                                   settings.ImageUsage,
	})
}

func isAdminRequest(r *http.Request) bool {
	actor, ok := service.RequestActorFromContext(r.Context())
	return ok && actor.Role == "admin"
}

package service

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

const systemSettingsConfigKey = "system_settings"

var ErrInvalidSystemSettings = errors.New("invalid system settings")

type SystemSettings struct {
	PublicStatistics                          bool           `json:"public_statistics"`
	TitleEnabled                              bool           `json:"title_enabled"`
	Title                                     string         `json:"title"`
	ExternalRegistrationEnabled               bool           `json:"external_registration_enabled"`
	EmailVerificationEnabled                  bool           `json:"email_verification_enabled"`
	EmailProvider                             string         `json:"email_provider"`
	EmailProviderOptions                      []OptionItem   `json:"email_provider_options"`
	RegistrationEmailDomainRestrictionEnabled bool           `json:"registration_email_domain_restriction_enabled"`
	RegistrationEmailDomains                  string         `json:"registration_email_domains"`
	NtfyPrivateURLPolicy                      string         `json:"ntfy_private_url_policy"`
	APIKeyLimitPerUser                        int            `json:"api_key_limit_per_user"`
	RealtimeMaxConnections                    int            `json:"realtime_max_connections"`
	RealtimeMaxConnectionsPerUser             int            `json:"realtime_max_connections_per_user"`
	RealtimeQueueSize                         int            `json:"realtime_queue_size"`
	ImageMaxSingleBytes                       int64          `json:"image_max_single_bytes"`
	ImageMaxRequestBytes                      int64          `json:"image_max_request_bytes"`
	ImageMaxTotalBytes                        int64          `json:"image_max_total_bytes"`
	StorageBlockNewConversations              bool           `json:"storage_block_new_conversations"`
	PendingMaxPerUser                         int            `json:"pending_max_per_user"`
	PendingMaxAgeHours                        int            `json:"pending_max_age_hours"`
	PendingMaxOutputChars                     int            `json:"pending_max_output_chars"`
	PendingAutoAbortMessage                   string         `json:"pending_auto_abort_message"`
	ImageUsage                                ImageUsageInfo `json:"image_usage"`
}

type OptionItem struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type ImageUsageInfo struct {
	TotalBytes  int64 `json:"total_bytes"`
	FileCount   int   `json:"file_count"`
	OrphanBytes int64 `json:"orphan_bytes"`
	OrphanCount int   `json:"orphan_count"`
}

type SystemSettingsService struct {
	store store.Store
	cfg   config.Config
}

func NewSystemSettingsService(dataStore store.Store, cfg config.Config) *SystemSettingsService {
	return &SystemSettingsService{store: dataStore, cfg: cfg}
}

func (s *SystemSettingsService) Schema() ConfigSchema {
	defaults := defaultSystemSettings(s.cfg)
	emailProviderValues := make([]string, 0, len(defaults.EmailProviderOptions))
	for _, item := range defaults.EmailProviderOptions {
		if strings.TrimSpace(item.Value) != "" {
			emailProviderValues = append(emailProviderValues, strings.TrimSpace(item.Value))
		}
	}
	return ConfigSchema{
		Fields: []ConfigFieldSchema{
			{Key: "public_statistics", ValueType: "boolean", DefaultValue: defaults.PublicStatistics, Public: true, AdminWriteOnly: true, Description: "Expose aggregate statistics to non-admin users."},
			{Key: "title_enabled", ValueType: "boolean", DefaultValue: defaults.TitleEnabled, Public: true, AdminWriteOnly: true, Description: "Whether a custom title is shown in the UI."},
			{Key: "title", ValueType: "string", DefaultValue: defaults.Title, Public: true, AdminWriteOnly: true, Description: "Custom UI title."},
			{Key: "external_registration_enabled", ValueType: "boolean", DefaultValue: defaults.ExternalRegistrationEnabled, Public: true, AdminWriteOnly: true, Description: "Whether public registration is enabled."},
			{Key: "email_verification_enabled", ValueType: "boolean", DefaultValue: defaults.EmailVerificationEnabled, Public: true, AdminWriteOnly: true, Description: "Whether registration and password reset require email verification."},
			{Key: "email_provider", ValueType: "string", DefaultValue: defaults.EmailProvider, Public: true, AdminWriteOnly: true, Description: "Selected outbound email provider.", Validation: map[string]any{"allowed_values": emailProviderValues}},
			{Key: "email_provider_options", ValueType: "object_array", DefaultValue: defaults.EmailProviderOptions, Public: true, AdminWriteOnly: true, ReadOnly: true, Description: "Providers available from current runtime configuration."},
			{Key: "registration_email_domain_restriction_enabled", ValueType: "boolean", DefaultValue: defaults.RegistrationEmailDomainRestrictionEnabled, Public: true, AdminWriteOnly: true, Description: "Whether registration email domains are restricted."},
			{Key: "registration_email_domains", ValueType: "string", DefaultValue: defaults.RegistrationEmailDomains, Public: true, AdminWriteOnly: true, Description: "Comma-separated email domains allowed for registration."},
			{Key: "ntfy_private_url_policy", ValueType: "string", DefaultValue: defaults.NtfyPrivateURLPolicy, Public: true, AdminWriteOnly: true, Description: "Policy for private ntfy URLs.", Validation: map[string]any{"allowed_values": []string{"disabled", "admin", "all"}}},
			{Key: "api_key_limit_per_user", ValueType: "integer", DefaultValue: defaults.APIKeyLimitPerUser, Public: true, AdminWriteOnly: true, Description: "Maximum number of API keys a user may hold.", Validation: map[string]any{"min": 0}},
			{Key: "realtime_max_connections", ValueType: "integer", DefaultValue: defaults.RealtimeMaxConnections, Public: true, AdminWriteOnly: true, Description: "Global realtime connection cap.", Validation: map[string]any{"min": 0}},
			{Key: "realtime_max_connections_per_user", ValueType: "integer", DefaultValue: defaults.RealtimeMaxConnectionsPerUser, Public: true, AdminWriteOnly: true, Description: "Per-user realtime connection cap.", Validation: map[string]any{"min": 0}},
			{Key: "realtime_queue_size", ValueType: "integer", DefaultValue: defaults.RealtimeQueueSize, Public: true, AdminWriteOnly: true, Description: "Per-subscriber realtime event queue size.", Validation: map[string]any{"min": 1}},
			{Key: "image_max_single_bytes", ValueType: "integer", DefaultValue: defaults.ImageMaxSingleBytes, Public: true, AdminWriteOnly: true, Description: "Maximum bytes for a single uploaded image.", Validation: map[string]any{"min": 0}},
			{Key: "image_max_request_bytes", ValueType: "integer", DefaultValue: defaults.ImageMaxRequestBytes, Public: true, AdminWriteOnly: true, Description: "Maximum bytes accepted by one image upload request.", Validation: map[string]any{"min": 0}},
			{Key: "image_max_total_bytes", ValueType: "integer", DefaultValue: defaults.ImageMaxTotalBytes, Public: true, AdminWriteOnly: true, Description: "Default total image storage quota per user.", Validation: map[string]any{"min": 0}},
			{Key: "storage_block_new_conversations", ValueType: "boolean", DefaultValue: defaults.StorageBlockNewConversations, Public: true, AdminWriteOnly: true, Description: "Block new conversations when a user is already over the effective storage quota."},
			{Key: "pending_max_per_user", ValueType: "integer", DefaultValue: defaults.PendingMaxPerUser, Public: true, AdminWriteOnly: true, Description: "Maximum active pending turns per user.", Validation: map[string]any{"min": 0}},
			{Key: "pending_max_age_hours", ValueType: "integer", DefaultValue: defaults.PendingMaxAgeHours, Public: true, AdminWriteOnly: true, Description: "Maximum pending turn age before cleanup.", Validation: map[string]any{"min": 0}},
			{Key: "pending_max_output_chars", ValueType: "integer", DefaultValue: defaults.PendingMaxOutputChars, Public: true, AdminWriteOnly: true, Description: "Maximum draft output length kept in memory.", Validation: map[string]any{"min": 0}},
			{Key: "pending_auto_abort_message", ValueType: "string", DefaultValue: defaults.PendingAutoAbortMessage, Public: true, AdminWriteOnly: true, Description: "Abort message returned when a pending turn expires."},
			{Key: "image_usage", ValueType: "object", DefaultValue: ImageUsageInfo{}, Public: true, AdminWriteOnly: true, ReadOnly: true, Description: "Current image storage usage summary."},
		},
	}
}

func (s *SystemSettingsService) Get(ctx context.Context) (SystemSettings, error) {
	if s == nil || s.store == nil {
		return SystemSettings{}, ErrInvalidSystemSettings
	}
	out := defaultSystemSettings(s.cfg)
	item, err := s.store.GetSystemConfig(ctx, systemSettingsConfigKey)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return SystemSettings{}, err
	}
	if err == nil {
		applySystemSettingsMap(&out, item.Value)
	}
	imageUsage, err := s.imageUsage(ctx)
	if err != nil {
		return SystemSettings{}, err
	}
	out.ImageUsage = imageUsage
	return out, nil
}

func (s *SystemSettingsService) Set(ctx context.Context, input map[string]any) (SystemSettings, error) {
	if s == nil || s.store == nil {
		return SystemSettings{}, ErrInvalidSystemSettings
	}
	current, err := s.Get(ctx)
	if err != nil {
		return SystemSettings{}, err
	}
	applySystemSettingsMap(&current, input)
	if _, err := s.store.SetSystemConfig(ctx, store.SetSystemConfigInput{
		Key:   systemSettingsConfigKey,
		Value: systemSettingsMap(current),
	}); err != nil {
		return SystemSettings{}, err
	}
	return s.Get(ctx)
}

func (s *SystemSettingsService) imageUsage(ctx context.Context) (ImageUsageInfo, error) {
	items, err := s.store.ListUploadedImages(ctx)
	if err != nil {
		return ImageUsageInfo{}, err
	}
	var usage ImageUsageInfo
	for _, item := range items {
		usage.TotalBytes += item.Bytes
		usage.FileCount++
	}
	return usage, nil
}

func defaultSystemSettings(cfg config.Config) SystemSettings {
	emailProviders := []OptionItem{}
	if cfg.SMTPEnabled {
		emailProviders = append(emailProviders, OptionItem{Value: "smtp", Label: "SMTP"})
	}
	pendingAgeHours := 48
	if cfg.PendingTurnTTL > 0 {
		pendingAgeHours = int(cfg.PendingTurnTTL.Hours())
	}
	return SystemSettings{
		PublicStatistics:            false,
		TitleEnabled:                false,
		Title:                       "",
		ExternalRegistrationEnabled: false,
		EmailVerificationEnabled:    false,
		EmailProvider:               "",
		EmailProviderOptions:        emailProviders,
		RegistrationEmailDomainRestrictionEnabled: false,
		RegistrationEmailDomains:                  "",
		NtfyPrivateURLPolicy:                      "disabled",
		APIKeyLimitPerUser:                        0,
		RealtimeMaxConnections:                    cfg.RealtimeMaxConnections,
		RealtimeMaxConnectionsPerUser:             cfg.RealtimeMaxConnectionsPerUser,
		RealtimeQueueSize:                         100,
		ImageMaxSingleBytes:                       cfg.UploadMaxBytes,
		ImageMaxRequestBytes:                      cfg.UploadMaxBytes,
		ImageMaxTotalBytes:                        cfg.StorageDefaultQuotaBytes,
		StorageBlockNewConversations:              cfg.StorageBlockNewConversations,
		PendingMaxPerUser:                         10,
		PendingMaxAgeHours:                        pendingAgeHours,
		PendingMaxOutputChars:                     300,
		PendingAutoAbortMessage:                   "本次回复等待超过限制，已自动结束，请重新发送。",
		ImageUsage:                                ImageUsageInfo{},
	}
}

func applySystemSettingsMap(target *SystemSettings, values map[string]any) {
	if target == nil {
		return
	}
	if value, ok := values["public_statistics"]; ok {
		target.PublicStatistics = settingsBool(value, target.PublicStatistics)
	}
	if value, ok := values["title_enabled"]; ok {
		target.TitleEnabled = settingsBool(value, target.TitleEnabled)
	}
	if value, ok := values["title"]; ok {
		target.Title = settingsString(value, target.Title)
	}
	if value, ok := values["external_registration_enabled"]; ok {
		target.ExternalRegistrationEnabled = settingsBool(value, target.ExternalRegistrationEnabled)
	}
	if value, ok := values["email_verification_enabled"]; ok {
		target.EmailVerificationEnabled = settingsBool(value, target.EmailVerificationEnabled)
	}
	if value, ok := values["email_provider"]; ok {
		target.EmailProvider = settingsString(value, target.EmailProvider)
	}
	if value, ok := values["registration_email_domain_restriction_enabled"]; ok {
		target.RegistrationEmailDomainRestrictionEnabled = settingsBool(value, target.RegistrationEmailDomainRestrictionEnabled)
	}
	if value, ok := values["registration_email_domains"]; ok {
		target.RegistrationEmailDomains = settingsString(value, target.RegistrationEmailDomains)
	}
	if value, ok := values["ntfy_private_url_policy"]; ok {
		target.NtfyPrivateURLPolicy = normalizePolicy(settingsString(value, target.NtfyPrivateURLPolicy))
	}
	if value, ok := values["api_key_limit_per_user"]; ok {
		target.APIKeyLimitPerUser = settingsInt(value, target.APIKeyLimitPerUser)
	}
	if value, ok := values["realtime_max_connections"]; ok {
		target.RealtimeMaxConnections = settingsInt(value, target.RealtimeMaxConnections)
	}
	if value, ok := values["realtime_max_connections_per_user"]; ok {
		target.RealtimeMaxConnectionsPerUser = settingsInt(value, target.RealtimeMaxConnectionsPerUser)
	}
	if value, ok := values["realtime_queue_size"]; ok {
		target.RealtimeQueueSize = settingsInt(value, target.RealtimeQueueSize)
	}
	if value, ok := values["image_max_single_bytes"]; ok {
		target.ImageMaxSingleBytes = settingsInt64(value, target.ImageMaxSingleBytes)
	}
	if value, ok := values["image_max_request_bytes"]; ok {
		target.ImageMaxRequestBytes = settingsInt64(value, target.ImageMaxRequestBytes)
	}
	if value, ok := values["image_max_total_bytes"]; ok {
		target.ImageMaxTotalBytes = settingsInt64(value, target.ImageMaxTotalBytes)
	}
	if value, ok := values["storage_block_new_conversations"]; ok {
		target.StorageBlockNewConversations = settingsBool(value, target.StorageBlockNewConversations)
	}
	if value, ok := values["pending_max_per_user"]; ok {
		target.PendingMaxPerUser = settingsInt(value, target.PendingMaxPerUser)
	}
	if value, ok := values["pending_max_age_hours"]; ok {
		target.PendingMaxAgeHours = settingsInt(value, target.PendingMaxAgeHours)
	}
	if value, ok := values["pending_max_output_chars"]; ok {
		target.PendingMaxOutputChars = settingsInt(value, target.PendingMaxOutputChars)
	}
	if value, ok := values["pending_auto_abort_message"]; ok {
		target.PendingAutoAbortMessage = settingsString(value, target.PendingAutoAbortMessage)
	}
}

func systemSettingsMap(input SystemSettings) map[string]any {
	return map[string]any{
		"public_statistics":             input.PublicStatistics,
		"title_enabled":                 input.TitleEnabled,
		"title":                         input.Title,
		"external_registration_enabled": input.ExternalRegistrationEnabled,
		"email_verification_enabled":    input.EmailVerificationEnabled,
		"email_provider":                input.EmailProvider,
		"registration_email_domain_restriction_enabled": input.RegistrationEmailDomainRestrictionEnabled,
		"registration_email_domains":                    input.RegistrationEmailDomains,
		"ntfy_private_url_policy":                       normalizePolicy(input.NtfyPrivateURLPolicy),
		"api_key_limit_per_user":                        input.APIKeyLimitPerUser,
		"realtime_max_connections":                      input.RealtimeMaxConnections,
		"realtime_max_connections_per_user":             input.RealtimeMaxConnectionsPerUser,
		"realtime_queue_size":                           input.RealtimeQueueSize,
		"image_max_single_bytes":                        input.ImageMaxSingleBytes,
		"image_max_request_bytes":                       input.ImageMaxRequestBytes,
		"image_max_total_bytes":                         input.ImageMaxTotalBytes,
		"storage_block_new_conversations":               input.StorageBlockNewConversations,
		"pending_max_per_user":                          input.PendingMaxPerUser,
		"pending_max_age_hours":                         input.PendingMaxAgeHours,
		"pending_max_output_chars":                      input.PendingMaxOutputChars,
		"pending_auto_abort_message":                    input.PendingAutoAbortMessage,
	}
}

func normalizePolicy(value string) string {
	switch strings.TrimSpace(value) {
	case "admin", "all":
		return strings.TrimSpace(value)
	default:
		return "disabled"
	}
}

func settingsString(value any, fallback string) string {
	raw, ok := value.(string)
	if !ok {
		return fallback
	}
	return raw
}

func settingsBool(value any, fallback bool) bool {
	raw, ok := value.(bool)
	if !ok {
		return fallback
	}
	return raw
}

func settingsInt(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return fallback
	}
}

func settingsInt64(value any, fallback int64) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	default:
		return fallback
	}
}

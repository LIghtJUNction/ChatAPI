package sqlite

import (
	"database/sql"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

type requestScanner interface {
	Scan(dest ...any) error
}

type appAPIKeyScanner interface {
	Scan(dest ...any) error
}

type appAPIKeyAuditLogScanner interface {
	Scan(dest ...any) error
}

type modelAPIKeyScanner interface {
	Scan(dest ...any) error
}

type userScanner interface {
	Scan(dest ...any) error
}

type userIdentityScanner interface {
	Scan(dest ...any) error
}

type systemConfigScanner interface {
	Scan(dest ...any) error
}

type userConfigScanner interface {
	Scan(dest ...any) error
}

type automationRuleScanner interface {
	Scan(dest ...any) error
}

type uploadedImageScanner interface {
	Scan(dest ...any) error
}

type storageFileDeletionFailureScanner interface {
	Scan(dest ...any) error
}

type storageUserQuotaScanner interface {
	Scan(dest ...any) error
}

type auditLogScanner interface {
	Scan(dest ...any) error
}

func scanRequestRow(scanner requestScanner) (store.Request, error) {
	var item store.Request
	var createdAt string
	var updatedAt string
	var messageMetadataJSON string
	var conversationMetadataJSON string
	if err := scanner.Scan(
		&item.ConversationID,
		&createdAt,
		&messageMetadataJSON,
		&updatedAt,
		&conversationMetadataJSON,
	); err != nil {
		return store.Request{}, err
	}

	messageMetadata := parseJSONMap(messageMetadataJSON)
	requestDebug, _ := messageMetadata["request_debug"].(map[string]any)
	conversationMetadata := parseJSONMap(conversationMetadataJSON)

	item.RequestID = metadataString(requestDebug, "request_id", "")
	item.OwnerID = metadataString(conversationMetadata, "owner_id", "")
	item.ResponseID = metadataString(requestDebug, "response_id", "")
	item.RequestFormat = metadataString(requestDebug, "request_format", "")
	item.Model = metadataString(requestDebug, "model", "")
	item.InputText = metadataString(requestDebug, "input_text", "")
	item.RequestMethod = metadataString(requestDebug, "request_method", "")
	item.RequestPath = metadataString(requestDebug, "request_path", "")
	item.RequestQuery = parseStringSliceMap(requestDebug["request_query"])
	item.RequestHeaders = parseStringSliceMap(requestDebug["request_headers"])
	item.Status = metadataString(conversationMetadata, "realtime_status", "")
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	item.Metadata = messageMetadata
	item.RequestBody, _ = requestDebug["request_body"].(map[string]any)
	item.ToolSchemas, _ = requestDebug["tool_schemas"].([]any)
	item.InputParts = parseRequestInputParts(requestDebug["input_parts"])
	item.ToolChoice = parseRequestToolChoice(requestDebug["tool_choice"])
	item.ResponseFormat = parseRequestResponseFormat(requestDebug["response_format"])
	item.SystemText = metadataString(requestDebug, "system_text", "")
	item.DeveloperText = metadataString(requestDebug, "developer_text", "")
	item.AssistantText = metadataString(requestDebug, "assistant_text", "")
	return item, nil
}

func parseRequestInputParts(value any) []store.RequestInputPart {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	parts := make([]store.RequestInputPart, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		parts = append(parts, store.RequestInputPart{
			Type:      metadataString(record, "type", ""),
			Text:      metadataString(record, "text", ""),
			MediaType: metadataString(record, "media_type", ""),
			URL:       metadataString(record, "url", ""),
		})
	}
	return parts
}

func parseStringSliceMap(value any) map[string][]string {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string][]string, len(record))
	for key, raw := range record {
		items, ok := raw.([]any)
		if !ok {
			continue
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				continue
			}
			values = append(values, text)
		}
		if len(values) == 0 {
			continue
		}
		result[key] = values
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseRequestToolChoice(value any) store.RequestToolChoice {
	record, _ := value.(map[string]any)
	return store.RequestToolChoice{
		Type: metadataString(record, "type", ""),
		Name: metadataString(record, "name", ""),
	}
}

func parseRequestResponseFormat(value any) store.RequestResponseFormat {
	record, _ := value.(map[string]any)
	format := store.RequestResponseFormat{
		Type: metadataString(record, "type", ""),
		Name: metadataString(record, "name", ""),
	}
	format.Schema, _ = record["schema"].(map[string]any)
	return format
}

func scanAppAPIKey(scanner appAPIKeyScanner) (store.AppAPIKey, error) {
	var item store.AppAPIKey
	var scopesJSON string
	var resourceLimitsJSON string
	var expiresAt sql.NullString
	var lastUsedAt sql.NullString
	var createdAt string
	var revokedAt sql.NullString
	if err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.Name,
		&item.KeyHash,
		&item.KeyPrefix,
		&scopesJSON,
		&resourceLimitsJSON,
		&expiresAt,
		&lastUsedAt,
		&createdAt,
		&revokedAt,
	); err != nil {
		return store.AppAPIKey{}, err
	}
	item.Scopes = parseJSONStringArray(scopesJSON)
	item.ResourceLimits = parseJSONMap(resourceLimitsJSON)
	if expiresAt.Valid {
		value := parseTime(expiresAt.String)
		item.ExpiresAt = &value
	}
	if lastUsedAt.Valid {
		value := parseTime(lastUsedAt.String)
		item.LastUsedAt = &value
	}
	item.CreatedAt = parseTime(createdAt)
	if revokedAt.Valid {
		value := parseTime(revokedAt.String)
		item.RevokedAt = &value
	}
	return item, nil
}

func scanAppAPIKeyAuditLog(scanner appAPIKeyAuditLogScanner) (store.AppAPIKeyAuditLog, error) {
	var item store.AppAPIKeyAuditLog
	var createdAt string
	if err := scanner.Scan(
		&item.ID,
		&item.AppAPIKeyID,
		&item.UserID,
		&item.Route,
		&item.StatusCode,
		&item.ErrorCode,
		&createdAt,
	); err != nil {
		return store.AppAPIKeyAuditLog{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

func scanModelAPIKey(scanner modelAPIKeyScanner) (store.ModelAPIKey, error) {
	var item store.ModelAPIKey
	var model string
	var lastUsedAt sql.NullString
	var createdAt string
	var revokedAt sql.NullString
	if err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.Name,
		&item.KeyCiphertext,
		&item.KeyPrefix,
		&model,
		&lastUsedAt,
		&createdAt,
		&revokedAt,
	); err != nil {
		return store.ModelAPIKey{}, err
	}
	item.Model = strings.TrimSpace(model)
	if lastUsedAt.Valid {
		value := parseTime(lastUsedAt.String)
		item.LastUsedAt = &value
	}
	item.CreatedAt = parseTime(createdAt)
	if revokedAt.Valid {
		value := parseTime(revokedAt.String)
		item.RevokedAt = &value
	}
	return item, nil
}

func scanUser(scanner userScanner) (store.User, error) {
	var item store.User
	var isActive int
	var localAdmin int
	var createdAt string
	var updatedAt string
	var lastLoginAt sql.NullString
	if err := scanner.Scan(
		&item.ID,
		&item.Username,
		&item.Email,
		&item.PasswordHash,
		&item.Role,
		&isActive,
		&localAdmin,
		&createdAt,
		&updatedAt,
		&lastLoginAt,
	); err != nil {
		return store.User{}, err
	}
	item.IsActive = isActive != 0
	item.LocalAdmin = localAdmin != 0
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	if lastLoginAt.Valid {
		value := parseTime(lastLoginAt.String)
		item.LastLoginAt = &value
	}
	return item, nil
}

func scanUserIdentity(scanner userIdentityScanner) (store.UserIdentity, error) {
	var item store.UserIdentity
	var emailVerified int
	var profileJSON string
	var createdAt string
	var updatedAt string
	var lastLoginAt sql.NullString
	if err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.Provider,
		&item.Subject,
		&item.Email,
		&emailVerified,
		&profileJSON,
		&createdAt,
		&updatedAt,
		&lastLoginAt,
	); err != nil {
		return store.UserIdentity{}, err
	}
	item.EmailVerified = emailVerified != 0
	item.Profile = parseJSONMap(profileJSON)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	if lastLoginAt.Valid {
		value := parseTime(lastLoginAt.String)
		item.LastLoginAt = &value
	}
	return item, nil
}

func scanSystemConfig(scanner systemConfigScanner) (store.SystemConfig, error) {
	var item store.SystemConfig
	var valueJSON string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&item.Key, &valueJSON, &createdAt, &updatedAt); err != nil {
		return store.SystemConfig{}, err
	}
	item.Value = parseJSONMap(valueJSON)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanUserConfig(scanner userConfigScanner) (store.UserConfig, error) {
	var item store.UserConfig
	var valueJSON string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&item.UserID, &item.Key, &valueJSON, &createdAt, &updatedAt); err != nil {
		return store.UserConfig{}, err
	}
	item.Value = parseJSONMap(valueJSON)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanAutomationRule(scanner automationRuleScanner) (store.AutomationRule, error) {
	var item store.AutomationRule
	var enabled int
	var payloadJSON string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&enabled,
		&payloadJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return store.AutomationRule{}, err
	}
	item.Enabled = enabled != 0
	item.Payload = parseJSONMap(payloadJSON)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

type authVerificationCodeScanner interface {
	Scan(dest ...any) error
}

func scanAuthVerificationCode(scanner authVerificationCodeScanner) (store.AuthVerificationCode, error) {
	var item store.AuthVerificationCode
	var lastSentAt string
	var expiresAt string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&item.Email, &item.Purpose, &item.CodeHash, &item.FailedAttempts, &expiresAt, &lastSentAt, &createdAt, &updatedAt); err != nil {
		return store.AuthVerificationCode{}, err
	}
	item.ExpiresAt = parseTime(expiresAt)
	item.LastSentAt = parseTime(lastSentAt)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanUploadedImage(scanner uploadedImageScanner) (store.UploadedImage, error) {
	var item store.UploadedImage
	var createdAt string
	if err := scanner.Scan(
		&item.ID,
		&item.OwnerID,
		&item.Filename,
		&item.OriginalFilename,
		&item.ContentType,
		&item.Bytes,
		&item.URL,
		&createdAt,
	); err != nil {
		return store.UploadedImage{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

type mediaAssetScanner interface {
	Scan(dest ...any) error
}

func scanMediaAsset(scanner mediaAssetScanner) (store.MediaAsset, error) {
	var item store.MediaAsset
	var createdAt string
	if err := scanner.Scan(
		&item.ID,
		&item.OwnerID,
		&item.FileID,
		&item.Path,
		&item.MediaType,
		&item.Bytes,
		&item.SHA256,
		&item.Width,
		&item.Height,
		&item.SourceKind,
		&item.OriginalName,
		&item.OriginalMediaType,
		&createdAt,
	); err != nil {
		return store.MediaAsset{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

func scanStorageFileDeletionFailure(scanner storageFileDeletionFailureScanner) (store.StorageFileDeletionFailure, error) {
	var item store.StorageFileDeletionFailure
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&item.Path,
		&item.Filename,
		&item.OwnerID,
		&item.Bytes,
		&item.LastError,
		&item.Attempts,
		&createdAt,
		&updatedAt,
	); err != nil {
		return store.StorageFileDeletionFailure{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanStorageUserQuota(scanner storageUserQuotaScanner) (store.StorageUserQuota, error) {
	var item store.StorageUserQuota
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&item.OwnerID,
		&item.QuotaBytes,
		&createdAt,
		&updatedAt,
	); err != nil {
		return store.StorageUserQuota{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanAuditLog(scanner auditLogScanner) (store.AuditLog, error) {
	var item store.AuditLog
	var metadataJSON string
	var createdAt string
	if err := scanner.Scan(
		&item.ID,
		&item.ActorUserID,
		&item.ActorRole,
		&item.ActorSource,
		&item.EventType,
		&item.ResourceType,
		&item.ResourceID,
		&item.Action,
		&item.Outcome,
		&item.IPAddress,
		&item.UserAgent,
		&metadataJSON,
		&createdAt,
	); err != nil {
		return store.AuditLog{}, err
	}
	item.Metadata = parseJSONMap(metadataJSON)
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

func isDraftWritable(metadata map[string]any) bool {
	status := metadataString(metadata, "realtime_status", "waiting")
	return status == "waiting" || status == "streaming"
}

func isTurnCompletable(metadata map[string]any) bool {
	status := metadataString(metadata, "realtime_status", "waiting")
	return status == "waiting" || status == "streaming"
}

func metadataString(metadata map[string]any, key string, fallback string) string {
	value, _ := metadata[key].(string)
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

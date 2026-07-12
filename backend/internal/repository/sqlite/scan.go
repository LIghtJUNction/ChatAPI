package sqlite

import (
	"database/sql"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
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

func scanRequestRow(scanner requestScanner) (common.Request, error) {
	var item common.Request
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
		return common.Request{}, err
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
	item.RawRequestBody, _ = requestDebug["raw_request_body"].(map[string]any)
	item.RequestOptions, _ = requestDebug["request_options"].(map[string]any)
	item.ToolSchemas, _ = requestDebug["tool_schemas"].([]any)
	item.BuiltinTools, _ = requestDebug["builtin_tools"].([]any)
	item.ToolChoice = parseRequestToolChoice(requestDebug["tool_choice"])
	item.ResponseFormat = parseRequestResponseFormat(requestDebug["response_format"])
	item.SystemText = metadataString(requestDebug, "system_text", "")
	item.DeveloperText = metadataString(requestDebug, "developer_text", "")
	item.AssistantText = metadataString(requestDebug, "assistant_text", "")
	return item, nil
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

func parseRequestToolChoice(value any) common.RequestToolChoice {
	record, _ := value.(map[string]any)
	return common.RequestToolChoice{
		Type: metadataString(record, "type", ""),
		Name: metadataString(record, "name", ""),
	}
}

func parseRequestResponseFormat(value any) common.RequestResponseFormat {
	record, _ := value.(map[string]any)
	format := common.RequestResponseFormat{
		Type: metadataString(record, "type", ""),
		Name: metadataString(record, "name", ""),
	}
	format.Schema, _ = record["schema"].(map[string]any)
	return format
}

func scanAppAPIKey(scanner appAPIKeyScanner) (common.AppAPIKey, error) {
	var item common.AppAPIKey
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
		return common.AppAPIKey{}, err
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

func scanAppAPIKeyAuditLog(scanner appAPIKeyAuditLogScanner) (common.AppAPIKeyAuditLog, error) {
	var item common.AppAPIKeyAuditLog
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
		return common.AppAPIKeyAuditLog{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

func scanModelAPIKey(scanner modelAPIKeyScanner) (common.ModelAPIKey, error) {
	var item common.ModelAPIKey
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
		return common.ModelAPIKey{}, err
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

func scanUser(scanner userScanner) (common.User, error) {
	var item common.User
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
		return common.User{}, err
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

func scanUserIdentity(scanner userIdentityScanner) (common.UserIdentity, error) {
	var item common.UserIdentity
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
		return common.UserIdentity{}, err
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

func scanSystemConfig(scanner systemConfigScanner) (common.SystemConfig, error) {
	var item common.SystemConfig
	var valueJSON string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&item.Key, &valueJSON, &createdAt, &updatedAt); err != nil {
		return common.SystemConfig{}, err
	}
	item.Value = parseJSONMap(valueJSON)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanUserConfig(scanner userConfigScanner) (common.UserConfig, error) {
	var item common.UserConfig
	var valueJSON string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&item.UserID, &item.Key, &valueJSON, &createdAt, &updatedAt); err != nil {
		return common.UserConfig{}, err
	}
	item.Value = parseJSONMap(valueJSON)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanAutomationRule(scanner automationRuleScanner) (common.AutomationRule, error) {
	var item common.AutomationRule
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
		return common.AutomationRule{}, err
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

func scanAuthVerificationCode(scanner authVerificationCodeScanner) (common.AuthVerificationCode, error) {
	var item common.AuthVerificationCode
	var lastSentAt string
	var expiresAt string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(&item.Email, &item.Purpose, &item.CodeHash, &item.FailedAttempts, &expiresAt, &lastSentAt, &createdAt, &updatedAt); err != nil {
		return common.AuthVerificationCode{}, err
	}
	item.ExpiresAt = parseTime(expiresAt)
	item.LastSentAt = parseTime(lastSentAt)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanUploadedImage(scanner uploadedImageScanner) (common.UploadedImage, error) {
	var item common.UploadedImage
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
		return common.UploadedImage{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

type mediaAssetScanner interface {
	Scan(dest ...any) error
}

func scanMediaAsset(scanner mediaAssetScanner) (common.MediaAsset, error) {
	var item common.MediaAsset
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
		return common.MediaAsset{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

func scanStorageFileDeletionFailure(scanner storageFileDeletionFailureScanner) (common.StorageFileDeletionFailure, error) {
	var item common.StorageFileDeletionFailure
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
		return common.StorageFileDeletionFailure{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanStorageUserQuota(scanner storageUserQuotaScanner) (common.StorageUserQuota, error) {
	var item common.StorageUserQuota
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&item.OwnerID,
		&item.QuotaBytes,
		&createdAt,
		&updatedAt,
	); err != nil {
		return common.StorageUserQuota{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanAuditLog(scanner auditLogScanner) (common.AuditLog, error) {
	var item common.AuditLog
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
		return common.AuditLog{}, err
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

func isPendingRequestDisconnected(metadata map[string]any) bool {
	status := metadataString(metadata, "realtime_status", "waiting")
	return status == "disconnected"
}

func metadataString(metadata map[string]any, key string, fallback string) string {
	value, _ := metadata[key].(string)
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

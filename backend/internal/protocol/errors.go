package protocol

import (
	"fmt"
	"strings"
)

type RequestError struct {
	StatusCode int
	Type       string
	Code       string
	Message    string
	Param      string
}

func (e *RequestError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func InvalidRequest(message string, param string) *RequestError {
	return &RequestError{
		StatusCode: 400,
		Type:       "invalid_request_error",
		Code:       "invalid_request",
		Message:    message,
		Param:      param,
	}
}

func InternalError(message string) *RequestError {
	return &RequestError{
		StatusCode: 500,
		Type:       "internal_server_error",
		Code:       "internal_error",
		Message:    message,
	}
}

func ValidateRequest(protocolValue string, body map[string]any) error {
	if body == nil {
		return InvalidRequest("request body must be a JSON object", "")
	}
	if streamValue, ok := body["stream"]; ok {
		if _, ok := streamValue.(bool); !ok {
			return InvalidRequest("stream must be a boolean", "stream")
		}
	}
	request := ParseRequest(protocolValue, body)
	if len(request.InputParts) == 0 {
		switch request.Protocol {
		case ProtocolChatCompletions, ProtocolAnthropicMessages:
			return InvalidRequest("messages must include at least one user content part", "messages")
		default:
			return InvalidRequest("input must include at least one user content part", "input")
		}
	}
	if err := validateToolChoice(body, request); err != nil {
		return err
	}
	if err := validateResponseFormat(body, request); err != nil {
		return err
	}
	return nil
}

func validateToolChoice(body map[string]any, request TurnRequest) error {
	rawChoice, exists := body["tool_choice"]
	if !exists {
		return nil
	}
	switch typed := rawChoice.(type) {
	case string:
		switch strings.TrimSpace(typed) {
		case "", "auto", "required", "none", "any":
			return nil
		default:
			return InvalidRequest("tool_choice string must be one of auto|required|none|any", "tool_choice")
		}
	case map[string]any:
		if request.ToolChoice.Type == "" {
			return InvalidRequest("tool_choice.type is required", "tool_choice.type")
		}
		if request.ToolChoice.Type != "function" {
			return nil
		}
		if request.ToolChoice.Name == "" {
			return InvalidRequest("tool_choice.function.name is required when type=function", "tool_choice.function.name")
		}
		if len(request.ToolSchemas) > 0 && !toolSchemaContains(request.ToolSchemas, request.ToolChoice.Name) {
			return InvalidRequest("tool_choice.function.name must reference a declared tool", "tool_choice.function.name")
		}
		return nil
	default:
		return InvalidRequest("tool_choice must be a string or object", "tool_choice")
	}
}

func validateResponseFormat(body map[string]any, request TurnRequest) error {
	rawFormat, exists := body["response_format"]
	if !exists {
		return nil
	}
	record, ok := rawFormat.(map[string]any)
	if !ok {
		return InvalidRequest("response_format must be an object", "response_format")
	}
	formatType := strings.TrimSpace(request.ResponseFormat.Type)
	if formatType == "" {
		return InvalidRequest("response_format.type is required", "response_format.type")
	}
	if formatType != "json_schema" {
		return nil
	}
	schemaRecord, ok := record["json_schema"].(map[string]any)
	if !ok {
		return InvalidRequest("response_format.json_schema is required when type=json_schema", "response_format.json_schema")
	}
	if strings.TrimSpace(request.ResponseFormat.Name) == "" {
		return InvalidRequest("response_format.json_schema.name is required", "response_format.json_schema.name")
	}
	if _, ok := schemaRecord["schema"].(map[string]any); !ok {
		return InvalidRequest("response_format.json_schema.schema is required", "response_format.json_schema.schema")
	}
	return nil
}

func toolSchemaContains(items []any, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(stringValue(record["name"], "")) == target {
			return true
		}
		function, ok := record["function"].(map[string]any)
		if ok && strings.TrimSpace(stringValue(function["name"], "")) == target {
			return true
		}
	}
	return false
}

func BuildErrorBody(protocolValue string, err error) map[string]any {
	requestErr := normalizeRequestError(err)
	proto := ParseProtocol(protocolValue)
	switch proto {
	case ProtocolAnthropicMessages:
		return map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    requestErr.Type,
				"message": requestErr.Message,
			},
		}
	default:
		payload := map[string]any{
			"message": requestErr.Message,
			"type":    requestErr.Type,
			"code":    requestErr.Code,
		}
		if requestErr.Param != "" {
			payload["param"] = requestErr.Param
		}
		return map[string]any{"error": payload}
	}
}

func normalizeRequestError(err error) *RequestError {
	if typed, ok := err.(*RequestError); ok && typed != nil {
		return typed
	}
	message := "internal server error"
	if err != nil {
		message = err.Error()
	}
	return &RequestError{
		StatusCode: 500,
		Type:       "internal_server_error",
		Code:       "internal_error",
		Message:    message,
	}
}

func HTTPStatus(err error) int {
	if typed, ok := err.(*RequestError); ok && typed != nil && typed.StatusCode > 0 {
		return typed.StatusCode
	}
	return 500
}

func AbortError(protocolValue string, reason string) map[string]any {
	return BuildErrorBody(protocolValue, &RequestError{
		StatusCode: 409,
		Type:       "request_aborted",
		Code:       "request_aborted",
		Message:    reason,
	})
}

func InvalidJSONError(protocolValue string) map[string]any {
	return BuildErrorBody(protocolValue, InvalidRequest("invalid json body", "body"))
}

func WrapInternalError(message string, err error) error {
	if err == nil {
		return InternalError(message)
	}
	return InternalError(fmt.Sprintf("%s: %v", message, err))
}

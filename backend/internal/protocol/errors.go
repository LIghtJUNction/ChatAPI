package protocol

import "fmt"

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
	return nil
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

package common

import "time"

type Conversation struct {
	ID                 string         `json:"id"`
	Title              string         `json:"title"`
	LastUserText       string         `json:"last_user_text"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	LastMessageAt      time.Time      `json:"last_message_at"`
	MessageCount       int            `json:"message_count"`
	LastMessagePreview string         `json:"last_message_preview"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	ResponseID         string         `json:"-"`
}

type Message struct {
	ID         string         `json:"id"`
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	CreatedAt  time.Time      `json:"created_at"`
	Status     string         `json:"status,omitempty"`
	ResponseID *string        `json:"response_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type ConversationEvent struct {
	ID             string               `json:"id"`
	ConversationID string               `json:"conversation_id"`
	OwnerID        string               `json:"owner_id"`
	Type           string               `json:"type"`
	Level          string               `json:"level"`
	Title          string               `json:"title"`
	Detail         string               `json:"detail,omitempty"`
	RequestID      string               `json:"request_id,omitempty"`
	Metadata       map[string]any       `json:"metadata,omitempty"`
	MediaAssets    []EventMediaAssetRef `json:"media_assets,omitempty"`
	CreatedAt      time.Time            `json:"created_at"`
}

type TurnIdentity struct {
	OwnerID   string `json:"owner_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

type PendingTurnMutationResult struct {
	Conversation Conversation      `json:"conversation"`
	Message      Message           `json:"message"`
	Event        ConversationEvent `json:"event"`
}

type Request struct {
	RequestID      string              `json:"request_id"`
	OwnerID        string              `json:"owner_id,omitempty"`
	ConversationID string              `json:"conversation_id"`
	ResponseID     string              `json:"response_id,omitempty"`
	RequestFormat  string              `json:"request_format,omitempty"`
	Model          string              `json:"model,omitempty"`
	SystemText     string              `json:"system_text,omitempty"`
	DeveloperText  string              `json:"developer_text,omitempty"`
	AssistantText  string              `json:"assistant_text,omitempty"`
	InputText      string              `json:"input_text,omitempty"`
	Status         string              `json:"status,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
	RequestMethod  string              `json:"request_method,omitempty"`
	RequestPath    string              `json:"request_path,omitempty"`
	RequestQuery   map[string][]string `json:"request_query,omitempty"`
	RequestHeaders map[string][]string `json:"request_headers,omitempty"`
	RequestBody    map[string]any      `json:"request_body,omitempty"`
	// RawRequestBody is the captured request fact; projections below may be regenerated.
	RawRequestBody map[string]any `json:"raw_request_body,omitempty"`
	// RequestOptions is a normalized snapshot for debugging and filtering.
	RequestOptions map[string]any        `json:"request_options,omitempty"`
	ToolSchemas    []any                 `json:"tool_schemas,omitempty"`
	BuiltinTools   []any                 `json:"builtin_tools,omitempty"`
	ToolChoice     RequestToolChoice     `json:"tool_choice,omitempty"`
	ResponseFormat RequestResponseFormat `json:"response_format,omitempty"`
	Metadata       map[string]any        `json:"metadata,omitempty"`
}

type RequestToolChoice struct {
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}

type RequestResponseFormat struct {
	Type   string         `json:"type,omitempty"`
	Name   string         `json:"name,omitempty"`
	Schema map[string]any `json:"schema,omitempty"`
}

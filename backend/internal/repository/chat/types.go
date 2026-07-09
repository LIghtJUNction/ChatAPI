package chat

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

type Request struct {
	RequestID      string                `json:"request_id"`
	OwnerID        string                `json:"owner_id,omitempty"`
	ConversationID string                `json:"conversation_id"`
	ResponseID     string                `json:"response_id,omitempty"`
	RequestFormat  string                `json:"request_format,omitempty"`
	Model          string                `json:"model,omitempty"`
	SystemText     string                `json:"system_text,omitempty"`
	DeveloperText  string                `json:"developer_text,omitempty"`
	AssistantText  string                `json:"assistant_text,omitempty"`
	InputText      string                `json:"input_text,omitempty"`
	Status         string                `json:"status,omitempty"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
	RequestMethod  string                `json:"request_method,omitempty"`
	RequestPath    string                `json:"request_path,omitempty"`
	RequestQuery   map[string][]string   `json:"request_query,omitempty"`
	RequestHeaders map[string][]string   `json:"request_headers,omitempty"`
	RequestBody    map[string]any        `json:"request_body,omitempty"`
	ToolSchemas    []any                 `json:"tool_schemas,omitempty"`
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

type CreatePendingInput struct {
	ConversationID   string
	RequestID        string
	ResponseID       string
	OwnerID          string
	RequestFormat    string
	Model            string
	SystemContent    string
	DeveloperContent string
	AssistantContent string
	UserContent      string
	RequestMethod    string
	RequestPath      string
	RequestQuery     map[string][]string
	RequestHeaders   map[string][]string
	RequestBody      map[string]any
	ToolSchemas      []any
	ToolChoice       RequestToolChoice
	ResponseFormat   RequestResponseFormat
	PreparedImages   []CreatePendingImageAssetInput
}

type CreatePendingImageAssetInput struct {
	FileID            string
	Path              string
	MediaType         string
	Bytes             int64
	SHA256            string
	Width             int
	Height            int
	SourceKind        string
	OriginalName      string
	OriginalMediaType string
	InputPartIndex    int
}

type CompletePendingInput struct {
	ConversationID      string
	ResponseID          string
	OutputText          string
	Mode                string
	ToolName            string
	ToolCallID          string
	ToolOutput          string
	ReasoningStreamMode string
}

type UpdateDraftInput struct {
	ConversationID string
	DraftText      string
}

type AbortPendingInput struct {
	ConversationID string
	Reason         string
}

type DeleteConversationsResult struct {
	DeletedConversations int `json:"deleted_conversations"`
	DeletedMessages      int `json:"deleted_messages"`
	DeletedAssetRefs     int `json:"deleted_asset_refs"`
}

type ExpirePendingTurnsResult struct {
	ExpiredConversations int `json:"expired_conversations"`
}

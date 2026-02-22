package provider

// CompletionRequest is the unified request type across all providers.
type CompletionRequest struct {
	// Routing
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"` // force specific provider

	// Messages
	Messages []Message `json:"messages"`
	System   string    `json:"system,omitempty"` // system prompt (Anthropic-style)

	// Parameters
	MaxTokens   int      `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	Stop        []string `json:"stop,omitempty"`
	Stream      bool     `json:"stream,omitempty"`

	// Tool calling
	Tools      []Tool `json:"tools,omitempty"`
	ToolChoice any    `json:"tool_choice,omitempty"`

	// Structured output
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`

	// Extended thinking / reasoning
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// Nexus metadata (not sent to provider)
	TenantID string            `json:"-"`
	KeyID    string            `json:"-"`
	Metadata map[string]string `json:"-"` // user-defined labels
}

// ThinkingConfig controls extended thinking / reasoning behavior.
type ThinkingConfig struct {
	Enabled         bool `json:"enabled,omitempty"`
	BudgetTokens    int  `json:"budget_tokens,omitempty"`
	IncludeThinking bool `json:"include_thinking,omitempty"`
}

// Message represents a conversation message.
type Message struct {
	Role       string     `json:"role"` // system, user, assistant, tool
	Content    any        `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ContentPart for multimodal messages.
type ContentPart struct {
	Type     string `json:"type"` // text, image_url, image_base64
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Data     string `json:"data,omitempty"` // base64
	MimeType string `json:"mime_type,omitempty"`
}

// Tool definition.
type Tool struct {
	Type     string       `json:"type"` // function
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a function tool.
type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"` // JSON Schema
}

// ToolCall represents a tool invocation from the assistant.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

// ToolCallFunc is the function call details within a ToolCall.
type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ResponseFormat specifies the desired output format.
type ResponseFormat struct {
	Type   string `json:"type"` // text, json_object, json_schema
	Schema any    `json:"schema,omitempty"`
}

// EmbeddingRequest for text embeddings.
type EmbeddingRequest struct {
	Model    string   `json:"model"`
	Input    []string `json:"input"`
	TenantID string   `json:"-"`
}

package provider

import "context"

// Stream represents a streaming response from a provider.
type Stream interface {
	// Next returns the next chunk. Returns io.EOF when done.
	Next(ctx context.Context) (*StreamChunk, error)

	// Close releases resources.
	Close() error

	// Usage returns final usage after stream completes.
	Usage() *Usage
}

// StreamEvents is an optional interface streams may implement to expose
// a channel-based consumer surface in addition to the iterator-style Next().
// Use provider.NewChannelAdapter as a default fallback for streams that
// only implement Stream.
type StreamEvents interface {
	Events(ctx context.Context) <-chan StreamChunk
}

// EventKind identifies the semantic category of a StreamChunk.
//
// The zero value (EventDelta) is the legacy form: Delta carries Role/Content/
// ToolCalls and consumers ignore Kind. New kinds let providers surface richer
// signals (reasoning, message boundaries, usage, citations, multi-modal, etc.)
// without breaking existing consumers — kinds they don't recognize can be
// dropped or forwarded as-is.
type EventKind string

const (
	// EventDelta is the default — incremental Role/Content/ToolCalls.
	EventDelta EventKind = ""

	// EventMessageStart marks the beginning of an assistant message.
	EventMessageStart EventKind = "message_start"

	// EventMessageStop marks the end of an assistant message.
	EventMessageStop EventKind = "message_stop"

	// EventReasoning carries an extended-thinking / reasoning content delta.
	// Used for OpenAI o-series, Anthropic extended thinking, DeepSeek R1, etc.
	EventReasoning EventKind = "reasoning"

	// EventToolCallDelta carries an incremental tool-call fragment
	// (function name, partial arguments JSON, or accumulator-keyed index).
	EventToolCallDelta EventKind = "tool_call_delta"

	// EventAudio carries a multi-modal audio chunk (raw bytes or transcript).
	EventAudio EventKind = "audio"

	// EventImage carries a multi-modal image chunk.
	EventImage EventKind = "image"

	// EventCitation carries a single citation reference.
	EventCitation EventKind = "citation"

	// EventUsage carries a token-usage snapshot — typically the final chunk.
	EventUsage EventKind = "usage"

	// EventHeartbeat is an upstream keepalive ping (e.g. Anthropic "ping").
	// Encoders may translate this to a wire-level keepalive comment.
	EventHeartbeat EventKind = "heartbeat"

	// EventError signals a recoverable, in-band provider error. Consumers
	// should propagate to the wire but may continue reading further events.
	EventError EventKind = "error"
)

// StreamChunk is a single piece of a streamed response.
//
// Kind discriminates the payload: legacy chunks have Kind == "" (EventDelta)
// and read Delta.Content / Delta.ToolCalls. Newer kinds populate the matching
// optional fields (Usage, Err) or extended Delta fields (Reasoning, Audio…).
type StreamChunk struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	Kind         EventKind `json:"kind,omitempty"`
	Delta        Delta     `json:"delta"`
	FinishReason string    `json:"finish_reason,omitempty"`

	// Optional payloads — populated based on Kind.
	Usage   *Usage `json:"usage,omitempty"`   // EventUsage / final delta
	Err     string `json:"error,omitempty"`   // EventError
	Created int64  `json:"created,omitempty"` // unix ms; used for cache-replay timing
}

// Delta contains the incremental content in a stream chunk.
//
// Role/Content/ToolCalls are the legacy fields. The remaining fields are
// optional and populated by providers that surface richer streams.
type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// Reasoning carries extended-thinking content (o1, Claude thinking_delta,
	// DeepSeek R1 reasoning_content). Emitted alongside (or before) Content.
	Reasoning string `json:"reasoning,omitempty"`

	// Refusal is an OpenAI-style structured refusal string.
	Refusal string `json:"refusal,omitempty"`

	// Citations carries grounding / source references.
	Citations []Citation `json:"citations,omitempty"`

	// Audio carries a multi-modal audio chunk (raw bytes + optional transcript).
	Audio *AudioChunk `json:"audio,omitempty"`

	// Image carries a multi-modal image chunk (raw bytes or URL).
	Image *ImageChunk `json:"image,omitempty"`

	// Transcript is a partial speech transcript (realtime / live APIs).
	Transcript string `json:"transcript,omitempty"`
}

// Citation is a single source/grounding reference.
type Citation struct {
	URL      string `json:"url,omitempty"`
	Title    string `json:"title,omitempty"`
	Quoted   string `json:"quoted,omitempty"`
	StartIdx int    `json:"start_idx,omitempty"`
	EndIdx   int    `json:"end_idx,omitempty"`
}

// AudioChunk is a multi-modal audio fragment.
type AudioChunk struct {
	Format     string `json:"format,omitempty"` // "pcm16","opus","mp3"
	SampleRate int    `json:"sample_rate,omitempty"`
	Data       []byte `json:"data,omitempty"`       // raw bytes; encoders may base64
	Transcript string `json:"transcript,omitempty"` // partial transcript for realtime
}

// ImageChunk is a multi-modal image fragment.
type ImageChunk struct {
	MimeType string `json:"mime_type,omitempty"`
	Data     []byte `json:"data,omitempty"`
	URL      string `json:"url,omitempty"`
	Partial  bool   `json:"partial,omitempty"` // true while incremental, false on final
}

package provider

// Capabilities describes what a provider can do.
type Capabilities struct {
	Chat       bool `json:"chat"`       // Chat completions
	Streaming  bool `json:"streaming"`  // SSE streaming
	Embeddings bool `json:"embeddings"` // Text embeddings
	Images     bool `json:"images"`     // Image generation
	Vision     bool `json:"vision"`     // Image input in messages
	Tools      bool `json:"tools"`      // Function/tool calling
	JSON       bool `json:"json"`       // Structured JSON output
	Audio      bool `json:"audio"`      // Audio input/output
	Thinking   bool `json:"thinking"`   // Extended thinking / reasoning
	Batch      bool `json:"batch"`      // Batch API support

	// Streaming-specific capabilities — describe what the provider can
	// surface incrementally during a stream. Use these for feature
	// detection at the gateway (e.g. only forward reasoning deltas to
	// clients that explicitly opted in).
	StreamingReasoning bool `json:"streaming_reasoning,omitempty"` // emits delta.reasoning
	StreamingTools     bool `json:"streaming_tools,omitempty"`     // emits incremental tool-call deltas
	StreamingAudio     bool `json:"streaming_audio,omitempty"`     // emits delta.audio chunks
	StreamingCitations bool `json:"streaming_citations,omitempty"` // emits delta.citations
	RealtimeAudio      bool `json:"realtime_audio,omitempty"`      // bidirectional realtime audio (OpenAI Realtime)
	RealtimeVideo      bool `json:"realtime_video,omitempty"`      // bidirectional realtime video
	LiveBidi           bool `json:"live_bidi,omitempty"`           // bidirectional live API (Gemini Live)
}

// Supports checks if a capability is available by name.
func (c Capabilities) Supports(capability string) bool {
	switch capability {
	case "chat":
		return c.Chat
	case "streaming":
		return c.Streaming
	case "embeddings":
		return c.Embeddings
	case "images":
		return c.Images
	case "vision":
		return c.Vision
	case "tools":
		return c.Tools
	case "json":
		return c.JSON
	case "audio":
		return c.Audio
	case "thinking":
		return c.Thinking
	case "batch":
		return c.Batch
	case "streaming_reasoning":
		return c.StreamingReasoning
	case "streaming_tools":
		return c.StreamingTools
	case "streaming_audio":
		return c.StreamingAudio
	case "streaming_citations":
		return c.StreamingCitations
	case "realtime_audio":
		return c.RealtimeAudio
	case "realtime_video":
		return c.RealtimeVideo
	case "live_bidi":
		return c.LiveBidi
	default:
		return false
	}
}

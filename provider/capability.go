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
}

// Supports checks if a capability is available by name.
func (c Capabilities) Supports(cap string) bool {
	switch cap {
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
	default:
		return false
	}
}

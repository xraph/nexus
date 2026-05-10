package httpstream

// DefaultRegistry returns a registry pre-populated with the standard
// encoders:
//
//   - text/event-stream — SSE in OpenAI chat.completion.chunk envelope
//     (default; required for OpenAI SDK compatibility).
//   - application/vnd.nexus.events+sse — SSE with named events,
//     surfaces every kind (reasoning, tool_call, …) for nexus-aware clients.
//   - application/x-ndjson — line-delimited JSON of native StreamEvent.
//
// Aliases registered: "sse"/"openai" → OpenAI SSE; "nexus"/"nexus-sse" →
// native SSE; "ndjson"/"jsonl" → NDJSON.
func DefaultRegistry() *Registry {
	r := NewRegistry()

	openAI := NewSSEOpenAIEncoder()
	r.Register("text/event-stream", openAI)
	r.RegisterAlias("sse", "text/event-stream")
	r.RegisterAlias("openai", "text/event-stream")
	r.SetDefault("text/event-stream")

	native := NewSSENativeEncoder()
	r.Register("application/vnd.nexus.events+sse", native)
	r.RegisterAlias("nexus", "application/vnd.nexus.events+sse")
	r.RegisterAlias("nexus-sse", "application/vnd.nexus.events+sse")

	ndjson := NewNDJSONEncoder()
	r.Register("application/x-ndjson", ndjson)
	r.RegisterAlias("ndjson", "application/x-ndjson")
	r.RegisterAlias("jsonl", "application/x-ndjson")

	return r
}

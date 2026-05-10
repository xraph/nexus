package provider

import "time"

// NewUsageChunk builds a chunk that carries final/intermediate usage.
// Used by providers that surface usage as a separate SSE event (OpenAI with
// stream_options.include_usage, Anthropic message_delta) and by middleware
// that synthesizes a usage frame for downstream encoders.
func NewUsageChunk(providerName, model string, usage *Usage) *StreamChunk {
	return &StreamChunk{
		Provider: providerName,
		Model:    model,
		Kind:     EventUsage,
		Usage:    usage,
		Created:  time.Now().UnixMilli(),
	}
}

// NewErrorChunk builds an in-band error frame. The provider field tags which
// component produced the error so wire encoders can attribute it.
func NewErrorChunk(providerName string, err error) *StreamChunk {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return &StreamChunk{
		Provider: providerName,
		Kind:     EventError,
		Err:      msg,
		Created:  time.Now().UnixMilli(),
	}
}

// NewHeartbeatChunk builds a keepalive frame. Encoders may translate to a
// wire-level keepalive (SSE comment, WS ping) instead of forwarding the JSON.
func NewHeartbeatChunk(providerName string) *StreamChunk {
	return &StreamChunk{
		Provider: providerName,
		Kind:     EventHeartbeat,
		Created:  time.Now().UnixMilli(),
	}
}

// NewReasoningChunk builds a reasoning/thinking delta.
func NewReasoningChunk(providerName, model, reasoning string) *StreamChunk {
	return &StreamChunk{
		Provider: providerName,
		Model:    model,
		Kind:     EventReasoning,
		Delta:    Delta{Reasoning: reasoning},
		Created:  time.Now().UnixMilli(),
	}
}

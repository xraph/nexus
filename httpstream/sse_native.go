package httpstream

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// SSENativeEncoder emits events in nexus-native SSE form: each event uses a
// named SSE event (event: delta, event: reasoning, event: tool_call, etc.)
// and the data line is the raw StreamEvent JSON. This is more expressive
// than the OpenAI-compatible envelope — clients can route on event name
// without parsing the payload — but it's not compatible with OpenAI SDKs.
//
// Use this when you control the consumer (a Go SDK, a custom dashboard).
// Consumers in OpenAI-SDK ecosystems should keep the SSEOpenAIEncoder.
type SSENativeEncoder struct{}

// NewSSENativeEncoder returns a fresh native SSE encoder.
func NewSSENativeEncoder() *SSENativeEncoder { return &SSENativeEncoder{} }

func (e *SSENativeEncoder) ContentType() string { return "application/vnd.nexus.events+sse" }

func (e *SSENativeEncoder) WriteHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", e.ContentType())
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}

func (e *SSENativeEncoder) EncodeEvent(w io.Writer, ev *StreamEvent) error {
	if ev == nil {
		return nil
	}
	if ev.Type == EventTypeHeartbeat {
		return e.Heartbeat(w)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("httpstream: marshal native sse event: %w", err)
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
	return err
}

func (e *SSENativeEncoder) EncodeError(w io.Writer, werr *WireError) error {
	if werr == nil {
		return nil
	}
	return e.EncodeEvent(w, &StreamEvent{Type: EventTypeError, Err: werr})
}

func (e *SSENativeEncoder) Heartbeat(w io.Writer) error {
	_, err := fmt.Fprintf(w, ": ping\n\n")
	return err
}

func (e *SSENativeEncoder) End(w io.Writer) error {
	return e.EncodeEvent(w, &StreamEvent{Type: EventTypeDone})
}

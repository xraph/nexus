package httpstream

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// NDJSONEncoder emits one JSON object per line, native to the StreamEvent
// shape (not OpenAI-compatible). Each line is a discriminated event with a
// `type` field. Useful for non-browser consumers (curl -N, Go HTTP clients).
//
// Example output:
//
//	{"type":"delta","model":"gpt-4o","delta":{"content":"Hello"}}
//	{"type":"reasoning","delta":{"reasoning":"Let me think..."}}
//	{"type":"tool_call","delta":{"tool_calls":[{"index":0,...}]}}
//	{"type":"usage","usage":{"prompt_tokens":5,...}}
//	{"type":"done"}
type NDJSONEncoder struct{}

// NewNDJSONEncoder returns a fresh NDJSON encoder.
func NewNDJSONEncoder() *NDJSONEncoder { return &NDJSONEncoder{} }

func (e *NDJSONEncoder) ContentType() string { return "application/x-ndjson" }

func (e *NDJSONEncoder) WriteHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", e.ContentType())
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}

func (e *NDJSONEncoder) EncodeEvent(w io.Writer, ev *StreamEvent) error {
	if ev == nil {
		return nil
	}
	// encoding/json marshals []byte as a base64-encoded JSON string by
	// default, so binary payloads (audio/image) are automatically safe for
	// line-delimited JSON without manual pre-encoding.
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("httpstream: marshal ndjson event: %w", err)
	}
	if _, writeErr := w.Write(data); writeErr != nil {
		return writeErr
	}
	_, err = w.Write([]byte("\n"))
	return err
}

func (e *NDJSONEncoder) EncodeError(w io.Writer, werr *WireError) error {
	if werr == nil {
		return nil
	}
	return e.EncodeEvent(w, &StreamEvent{Type: EventTypeError, Err: werr})
}

func (e *NDJSONEncoder) Heartbeat(w io.Writer) error {
	return e.EncodeEvent(w, &StreamEvent{Type: EventTypeHeartbeat})
}

func (e *NDJSONEncoder) End(w io.Writer) error {
	return e.EncodeEvent(w, &StreamEvent{Type: EventTypeDone})
}

// Package httpstream provides HTTP streaming wire formats for Nexus.
//
// It owns the encoders that translate provider.StreamChunk events into
// Server-Sent Events, NDJSON, WebSocket frames, or any custom format
// registered by the embedder. Encoders are pluggable via Registry; the
// proxy and api packages negotiate one based on the request's Accept
// header (or an explicit format override) and feed it to Run, which is
// the single shared event loop with heartbeats, cancellation, and
// sanitised error events.
package httpstream

import (
	"github.com/xraph/nexus/provider"
)

// EventType discriminates the wire-level event kind.
//
// EventType is the on-the-wire form; provider.EventKind is the in-process
// form. Encoders translate between them. Some types (Done, Heartbeat) have
// no provider counterpart — they're synthesised by the runner.
type EventType string

const (
	EventTypeDelta     EventType = "delta"
	EventTypeReasoning EventType = "reasoning"
	EventTypeToolCall  EventType = "tool_call"
	EventTypeAudio     EventType = "audio"
	EventTypeImage     EventType = "image"
	EventTypeCitation  EventType = "citation"
	EventTypeUsage     EventType = "usage"
	EventTypeError     EventType = "error"
	EventTypeHeartbeat EventType = "heartbeat"
	EventTypeDone      EventType = "done"
)

// StreamEvent is the encoder-facing representation of a chunk.
//
// Encoders should populate only the fields relevant to Type. Unset fields
// are omitted on the wire. ID/Model/RequestID are echoed when known.
type StreamEvent struct {
	Type         EventType            `json:"type"`
	ID           string               `json:"id,omitempty"`
	Model        string               `json:"model,omitempty"`
	RequestID    string               `json:"request_id,omitempty"`
	Delta        *provider.Delta      `json:"delta,omitempty"`
	Usage        *provider.Usage      `json:"usage,omitempty"`
	Citation     *provider.Citation   `json:"citation,omitempty"`
	Audio        *provider.AudioChunk `json:"audio,omitempty"`
	Image        *provider.ImageChunk `json:"image,omitempty"`
	FinishReason string               `json:"finish_reason,omitempty"`
	Err          *WireError           `json:"error,omitempty"`
}

// WireError is the sanitised error envelope. Internal error strings are
// intentionally not exposed; consumers see a Type, a request id, and a
// retryable flag.
type WireError struct {
	Message   string `json:"message"`
	Type      string `json:"type"`
	Code      string `json:"code,omitempty"`
	Retryable bool   `json:"retryable"`
	RequestID string `json:"request_id,omitempty"`
}

// FromChunk maps a provider.StreamChunk into a StreamEvent.
//
// Provider impls populate Delta with mixed semantics (text content + tool
// calls + reasoning), so the mapping picks an EventType based on the chunk
// Kind first, then the most specific delta field present.
func FromChunk(c *provider.StreamChunk, requestID string) *StreamEvent {
	if c == nil {
		return nil
	}
	ev := &StreamEvent{
		ID:           c.ID,
		Model:        c.Model,
		RequestID:    requestID,
		FinishReason: c.FinishReason,
	}

	switch c.Kind {
	case provider.EventUsage:
		ev.Type = EventTypeUsage
		ev.Usage = c.Usage
		return ev
	case provider.EventError:
		ev.Type = EventTypeError
		ev.Err = &WireError{
			Message: c.Err,
			Type:    "upstream",
		}
		return ev
	case provider.EventHeartbeat:
		ev.Type = EventTypeHeartbeat
		return ev
	case provider.EventReasoning:
		ev.Type = EventTypeReasoning
	case provider.EventToolCallDelta:
		ev.Type = EventTypeToolCall
	case provider.EventAudio:
		ev.Type = EventTypeAudio
		if c.Delta.Audio != nil {
			audio := *c.Delta.Audio
			ev.Audio = &audio
		}
	case provider.EventImage:
		ev.Type = EventTypeImage
		if c.Delta.Image != nil {
			img := *c.Delta.Image
			ev.Image = &img
		}
	case provider.EventCitation:
		ev.Type = EventTypeCitation
		if len(c.Delta.Citations) > 0 {
			cit := c.Delta.Citations[0]
			ev.Citation = &cit
		}
	case provider.EventMessageStart, provider.EventMessageStop:
		// Pass through as a delta with role only — encoders that care can
		// surface it; ones that don't get an empty delta they can drop.
		ev.Type = EventTypeDelta
	default:
		ev.Type = EventTypeDelta
	}

	delta := c.Delta
	ev.Delta = &delta

	// Surface usage if a delta chunk also carries it (some providers
	// piggyback).
	if c.Usage != nil {
		ev.Usage = c.Usage
	}
	return ev
}

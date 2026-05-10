package httpstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
)

// StreamEncoder writes StreamEvents to an HTTP response in a wire format.
//
// All methods may be called from a single goroutine — encoders need not be
// concurrency-safe. WriteHeaders runs once before the first event; End runs
// once after the last (or after an error event, before the connection
// closes). Heartbeat is best-effort: if writing fails, the runner gives up
// and lets the next data write surface the error.
type StreamEncoder interface {
	// ContentType is the canonical Content-Type produced by this encoder.
	ContentType() string

	// WriteHeaders writes status + headers and prepares the response for
	// streaming. Implementations should set Cache-Control: no-cache, the
	// Content-Type, and disable buffering by flushing the headers.
	WriteHeaders(w http.ResponseWriter)

	// EncodeEvent writes one event. Errors abort the stream.
	EncodeEvent(w io.Writer, e *StreamEvent) error

	// EncodeError writes a typed error event. Encoders that support named
	// events (SSE) emit a discriminated frame; NDJSON/WS just emit a
	// type:"error" object.
	EncodeError(w io.Writer, err *WireError) error

	// Heartbeat writes a keepalive frame (SSE comment, NDJSON heartbeat
	// line, or WS ping). May be a no-op for protocols with native pings.
	Heartbeat(w io.Writer) error

	// End writes any required terminator (SSE [DONE], NDJSON {"type":"done"})
	// and flushes. Does not close the underlying connection.
	End(w io.Writer) error
}

// Registry maps content types to encoders, plus aliases for convenience
// (e.g. "sse" → "text/event-stream"). Safe for concurrent registration
// during construction; treat as read-only after first request handled.
type Registry struct {
	mu       sync.RWMutex
	byType   map[string]StreamEncoder
	aliases  map[string]string // alias → canonical content type
	defaultT string
}

// NewRegistry creates an empty registry. Use DefaultRegistry for one
// pre-seeded with SSE-OpenAI / SSE-native / NDJSON encoders.
func NewRegistry() *Registry {
	return &Registry{
		byType:  make(map[string]StreamEncoder),
		aliases: make(map[string]string),
	}
}

// Register adds an encoder for a content type. The first registered encoder
// becomes the default unless SetDefault is called.
func (r *Registry) Register(contentType string, enc StreamEncoder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ct := strings.ToLower(strings.TrimSpace(contentType))
	r.byType[ct] = enc
	if r.defaultT == "" {
		r.defaultT = ct
	}
}

// RegisterAlias maps an alias name to a registered content type
// (e.g. "ndjson" → "application/x-ndjson").
func (r *Registry) RegisterAlias(alias, contentType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[strings.ToLower(strings.TrimSpace(alias))] = strings.ToLower(strings.TrimSpace(contentType))
}

// SetDefault picks the encoder used when no Accept / format override matches.
func (r *Registry) SetDefault(contentType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defaultT = strings.ToLower(strings.TrimSpace(contentType))
}

// Default returns the default encoder, if registered.
func (r *Registry) Default() StreamEncoder {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.defaultT == "" {
		return nil
	}
	return r.byType[r.defaultT]
}

// Lookup resolves a content type or alias to its encoder. Returns nil if
// neither matches.
func (r *Registry) Lookup(name string) StreamEncoder {
	r.mu.RLock()
	defer r.mu.RUnlock()
	key := strings.ToLower(strings.TrimSpace(name))
	if alias, ok := r.aliases[key]; ok {
		key = alias
	}
	return r.byType[key]
}

// SanitizeError converts an in-process error into a wire-safe envelope.
// Mapping rules: known nexus error types get their typed Code; everything
// else is bucketed as "internal" with a generic message so we don't leak
// stack/system details.
func SanitizeError(err error, requestID string) *WireError {
	if err == nil {
		return nil
	}
	we := &WireError{
		RequestID: requestID,
	}
	switch {
	case errors.Is(err, context.Canceled):
		we.Type = "canceled"
		we.Message = "request canceled"
		we.Retryable = false
	case errors.Is(err, context.DeadlineExceeded):
		we.Type = "timeout"
		we.Message = "request timed out"
		we.Retryable = true
	default:
		we.Type = "upstream"
		we.Message = err.Error()
		we.Retryable = false
	}
	return we
}

package middlewares

import (
	"context"
	"time"

	"github.com/xraph/nexus/pipeline"
)

// TimeoutMiddleware enforces a maximum request duration.
type TimeoutMiddleware struct {
	timeout time.Duration
}

// NewTimeout creates a timeout middleware.
func NewTimeout(timeout time.Duration) *TimeoutMiddleware {
	return &TimeoutMiddleware{timeout: timeout}
}

func (m *TimeoutMiddleware) Name() string  { return "timeout" }
func (m *TimeoutMiddleware) Priority() int { return 20 } // Very early, wraps entire pipeline

func (m *TimeoutMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.timeout <= 0 {
		return next(ctx)
	}

	// Streaming requests MUST NOT be deadline-wrapped here.
	//
	// `next(ctx)` for a stream returns a `provider.Stream` object that is
	// consumed by the caller AFTER this Process returns. With the
	// deferred cancel() below, the wrapped context would be canceled the
	// instant we return — propagating to the upstream HTTP request's
	// context and closing the response body mid-stream. The caller then
	// reads from already-buffered SSE data (a few seconds' worth in the
	// transport's bufio.Reader and the provider's scanner buffer) and
	// only sees the cancellation when the buffer drains, surfacing as a
	// puzzling "context canceled" error several seconds into what looked
	// like a healthy stream.
	//
	// For streams, the underlying HTTP client's own Timeout already
	// provides a backstop on the call itself; per-request lifetime
	// management is the caller's responsibility (the agent runner closes
	// the stream when it's done consuming).
	if req != nil && req.Type == pipeline.RequestStream {
		return next(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	return next(ctx)
}

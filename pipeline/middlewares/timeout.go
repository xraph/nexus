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

	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	return next(ctx)
}

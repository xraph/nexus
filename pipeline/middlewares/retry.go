package middlewares

import (
	"context"
	"time"

	"github.com/xraph/nexus/pipeline"
)

// RetryMiddleware retries failed requests with exponential backoff.
type RetryMiddleware struct {
	maxRetries int
	delay      time.Duration
	backoff    float64
}

// NewRetry creates a retry middleware.
func NewRetry(maxRetries int, delay time.Duration, backoff float64) *RetryMiddleware {
	return &RetryMiddleware{
		maxRetries: maxRetries,
		delay:      delay,
		backoff:    backoff,
	}
}

func (m *RetryMiddleware) Name() string  { return "retry" }
func (m *RetryMiddleware) Priority() int { return 340 } // Just before provider_call (350)

func (m *RetryMiddleware) Process(ctx context.Context, _ *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	var lastErr error
	delay := m.delay

	for attempt := 0; attempt <= m.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				delay = time.Duration(float64(delay) * m.backoff)
			}
		}

		resp, err := next(ctx)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

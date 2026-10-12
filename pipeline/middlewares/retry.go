package middlewares

import (
	"context"
	"errors"
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
		// Only the request's own context knows it has no time left. A
		// provider client's timeout also satisfies errors.Is(err,
		// context.DeadlineExceeded), and that one is worth another try.
		if !retryable(err) || ctx.Err() != nil {
			return nil, err
		}
	}

	return nil, lastErr
}

// retryable reports whether another attempt could succeed. A refusal or a
// guard block will refuse again, and a permanent error will fail again.
// Whether the request has run out of time is the caller's check, on its
// context.
func retryable(err error) bool {
	var refused pipeline.Refusal
	return !errors.As(err, &refused) && !pipeline.IsPermanent(err)
}

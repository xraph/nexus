// Package ratelimit counts requests and tokens in fixed windows for the
// quota stage's RPM and TPM checks.
package ratelimit

import (
	"context"
	"time"
)

// Decision is what Allow decided.
type Decision struct {
	// Allowed reports whether the window was within limit before this
	// charge. A charge of 0 checks without counting.
	Allowed bool
	// Count is the window's count after the charge.
	Count int64
	// RetryAfter is the time left in the window.
	RetryAfter time.Duration
}

// Limiter charges n against key's current fixed window of the given size.
// It always charges, even when it refuses, so a client that keeps retrying
// inside a window stays refused until the window ends.
type Limiter interface {
	Allow(ctx context.Context, key string, n, limit int64, window time.Duration) (Decision, error)
	Kind() string
}

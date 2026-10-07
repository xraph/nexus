package ratelimit

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Memory is the default limiter. Its windows live in this process, so with
// several replicas each one allows the full limit. Safe for concurrent use.
type Memory struct {
	mu        sync.Mutex
	now       func() time.Time
	windows   map[string]*memWindow
	nextSweep time.Time
}

type memWindow struct {
	start time.Time
	end   time.Time
	count int64
}

// MemoryOption configures Memory.
type MemoryOption func(*Memory)

// WithClock replaces time.Now, for tests.
func WithClock(now func() time.Time) MemoryOption { return func(m *Memory) { m.now = now } }

// NewMemory returns an in-process limiter.
func NewMemory(opts ...MemoryOption) *Memory {
	m := &Memory{now: time.Now, windows: map[string]*memWindow{}}
	for _, o := range opts {
		o(m)
	}
	return m
}

// sweepAt is how many windows Memory holds before it drops ended ones.
var sweepAt = 10000

// Allow charges n against key's current fixed window of the given size.
// It returns a Decision and an error. If window is <= 0 or n is < 0,
// Allow returns a zero Decision and an error.
func (m *Memory) Allow(_ context.Context, key string, n, limit int64, window time.Duration) (Decision, error) {
	if window <= 0 {
		return Decision{}, errors.New("window must be > 0")
	}
	if n < 0 {
		return Decision{}, errors.New("n must be >= 0")
	}

	now := m.now()
	start := now.Truncate(window)
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.windows) >= sweepAt && !now.Before(m.nextSweep) {
		for k, w := range m.windows {
			if !w.end.After(now) {
				delete(m.windows, k)
			}
		}
		m.nextSweep = now.Add(time.Minute)
	}
	w := m.windows[key]
	if w == nil || !w.start.Equal(start) {
		w = &memWindow{start: start, end: start.Add(window)}
		m.windows[key] = w
	}
	before := w.count
	w.count += n
	return Decision{Allowed: before < limit, Count: w.count, RetryAfter: w.end.Sub(now)}, nil
}

// Kind returns the type of this limiter.
func (m *Memory) Kind() string { return "memory" }

var _ Limiter = (*Memory)(nil)

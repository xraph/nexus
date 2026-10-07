package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Memory is the default limiter. Its windows live in this process, so with
// several replicas each one allows the full limit.
type Memory struct {
	mu      sync.Mutex
	now     func() time.Time
	windows map[string]*memWindow
}

type memWindow struct {
	start time.Time
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
const sweepAt = 10000

func (m *Memory) Allow(_ context.Context, key string, n, limit int64, window time.Duration) (Decision, error) {
	now := m.now()
	start := now.Truncate(window)
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.windows) >= sweepAt {
		for k, w := range m.windows {
			if w.start.Add(window).Before(now) {
				delete(m.windows, k)
			}
		}
	}
	w := m.windows[key]
	if w == nil || !w.start.Equal(start) {
		w = &memWindow{start: start}
		m.windows[key] = w
	}
	before := w.count
	w.count += n
	return Decision{Allowed: before < limit, Count: w.count, RetryAfter: start.Add(window).Sub(now)}, nil
}

func (m *Memory) Kind() string { return "memory" }

var _ Limiter = (*Memory)(nil)

// Package redislimit is a ratelimit.Limiter whose windows live in Redis, so
// every replica shares them.
package redislimit

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/xraph/nexus/ratelimit"
)

// allow adds ARGV[1] to the window key, sets its expiry the first time, and
// returns the new count.
var allow = redis.NewScript(`
local n = redis.call('INCRBY', KEYS[1], ARGV[1])
if redis.call('PTTL', KEYS[1]) < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return n
`)

// Limiter is a Redis-backed fixed-window limiter.
type Limiter struct {
	c      redis.UniversalClient
	prefix string
	now    func() time.Time
}

// Option configures the limiter.
type Option func(*Limiter)

// WithPrefix sets the key prefix (default "nexus:rl:").
func WithPrefix(p string) Option { return func(l *Limiter) { l.prefix = p } }

// WithClock replaces time.Now, for tests.
func WithClock(now func() time.Time) Option { return func(l *Limiter) { l.now = now } }

// New returns a limiter on c. Windows are aligned to the clock of the replica
// that charges them, so keep replica clocks in sync.
func New(c redis.UniversalClient, opts ...Option) *Limiter {
	l := &Limiter{c: c, prefix: "nexus:rl:", now: time.Now}
	for _, o := range opts {
		o(l)
	}
	return l
}

// Allow charges n against key's current fixed window of the given size.
// If window is <= 0 or n is < 0, it returns a zero Decision and an error, as
// the memory limiter does. A Redis failure is returned as an error too.
func (l *Limiter) Allow(ctx context.Context, key string, n, limit int64, window time.Duration) (ratelimit.Decision, error) {
	if window <= 0 {
		return ratelimit.Decision{}, errors.New("window must be > 0")
	}
	if n < 0 {
		return ratelimit.Decision{}, errors.New("n must be >= 0")
	}

	now := l.now()
	start := now.Truncate(window)
	k := l.prefix + key + ":" + strconv.FormatInt(start.UnixMilli(), 10)
	count, err := allow.Run(ctx, l.c, []string{k}, n, window.Milliseconds()+1000).Int64()
	if err != nil {
		return ratelimit.Decision{}, err
	}
	return ratelimit.Decision{Allowed: count-n < limit, Count: count, RetryAfter: start.Add(window).Sub(now)}, nil
}

// Kind returns the type of this limiter.
func (l *Limiter) Kind() string { return "redis" }

var _ ratelimit.Limiter = (*Limiter)(nil)

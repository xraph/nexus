package redislimit_test

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/ratelimit/redislimit"
)

func client(t *testing.T) redis.UniversalClient {
	t.Helper()
	addr := os.Getenv("NEXUS_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NEXUS_TEST_REDIS_ADDR not set")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("redis at %s: %v", addr, err)
	}
	return c
}

func TestRedisWindowMatchesTheMemoryOne(t *testing.T) {
	c := client(t)
	now := time.Date(2026, 10, 7, 12, 0, 10, 0, time.UTC)
	prefix := "nexus:test:" + id.NewRequestID().String() + ":"
	l := redislimit.New(c, redislimit.WithPrefix(prefix), redislimit.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if d, err := l.Allow(ctx, "rpm:t", 1, 3, time.Minute); err != nil || !d.Allowed {
			t.Fatalf("request %d = %+v, %v", i+1, d, err)
		}
	}
	d, err := l.Allow(ctx, "rpm:t", 1, 3, time.Minute)
	if err != nil || d.Allowed || d.RetryAfter != 50*time.Second || d.Count != 4 {
		t.Fatalf("fourth = %+v, %v", d, err)
	}
	windowStart := strconv.FormatInt(now.Truncate(time.Minute).UnixMilli(), 10)
	ttl, err := c.PTTL(ctx, prefix+"rpm:t:"+windowStart).Result()
	if err != nil || ttl <= 0 {
		t.Fatalf("window key ttl = %v, %v; every window key must expire", ttl, err)
	}
	now = now.Add(50 * time.Second)
	if d, _ := l.Allow(ctx, "rpm:t", 1, 3, time.Minute); !d.Allowed || d.Count != 1 {
		t.Fatalf("new window = %+v", d)
	}
	if l.Kind() != "redis" {
		t.Fatal(l.Kind())
	}
}

func TestAnUnreachableRedisIsAnError(t *testing.T) {
	c := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = c.Close() })
	if _, err := redislimit.New(c).Allow(context.Background(), "k", 1, 1, time.Minute); err == nil {
		t.Fatal("Allow against an unreachable redis must return an error, so the quota stage can let the request through and count it")
	}
}

// The memory limiter refuses a non-positive window and a negative charge with
// a zero Decision. The Redis limiter must do the same, and must do it before
// it touches the network, so this test needs no Redis.
func TestABadWindowOrChargeIsRefusedBeforeRedisIsTouched(t *testing.T) {
	c := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = c.Close() })
	l := redislimit.New(c)
	cases := []struct {
		name   string
		n      int64
		window time.Duration
		want   string
	}{
		{"zero window", 1, 0, "window must be > 0"},
		{"negative window", 1, -time.Minute, "window must be > 0"},
		{"negative charge", -1, time.Minute, "n must be >= 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := l.Allow(context.Background(), "k", tc.n, 5, tc.window)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if d.Allowed || d.Count != 0 || d.RetryAfter != 0 {
				t.Fatalf("decision = %+v, want zero", d)
			}
		})
	}
}

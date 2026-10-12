package ratelimit_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xraph/nexus/ratelimit"
)

func TestFixedWindowAllowsUpToTheLimitThenRefuses(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 10, 0, time.UTC)
	l := ratelimit.NewMemory(ratelimit.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		d, err := l.Allow(ctx, "rpm:t", 1, 3, time.Minute)
		if err != nil || !d.Allowed {
			t.Fatalf("request %d = %+v, %v", i+1, d, err)
		}
	}
	d, _ := l.Allow(ctx, "rpm:t", 1, 3, time.Minute)
	if d.Allowed || d.RetryAfter != 50*time.Second {
		t.Fatalf("fourth = %+v, want refused with 50s left in the window", d)
	}
	now = now.Add(50 * time.Second) // the next window
	if d, _ := l.Allow(ctx, "rpm:t", 1, 3, time.Minute); !d.Allowed || d.Count != 1 {
		t.Fatalf("new window = %+v", d)
	}
}

func TestAZeroChargeChecksWithoutCounting(t *testing.T) {
	l := ratelimit.NewMemory()
	ctx := context.Background()
	if d, _ := l.Allow(ctx, "tpm:t", 0, 100, time.Minute); !d.Allowed || d.Count != 0 {
		t.Fatalf("check = %+v", d)
	}
	_, _ = l.Allow(ctx, "tpm:t", 150, 100, time.Minute) // a big request finishing
	if d, _ := l.Allow(ctx, "tpm:t", 0, 100, time.Minute); d.Allowed {
		t.Fatalf("after 150 of 100 tokens, check = %+v; want refused", d)
	}
}

func TestKeysAreIndependentAndSafeConcurrently(t *testing.T) {
	l := ratelimit.NewMemory()
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if d, _ := l.Allow(ctx, "rpm:a", 1, 50, time.Minute); d.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 50 {
		t.Fatalf("allowed %d of 200 against a limit of 50", allowed)
	}
	if d, _ := l.Allow(ctx, "rpm:b", 1, 50, time.Minute); !d.Allowed {
		t.Fatal("another key must have its own window")
	}
	if l.Kind() != "memory" {
		t.Fatal(l.Kind())
	}
}

func TestErrorOnInvalidWindow(t *testing.T) {
	l := ratelimit.NewMemory()
	ctx := context.Background()
	d, err := l.Allow(ctx, "key", 1, 100, 0)
	if err == nil {
		t.Fatalf("window=0 should error, got decision %+v", d)
	}
	if d != (ratelimit.Decision{}) {
		t.Fatalf("window=0 should return zero Decision, got %+v", d)
	}
	d, err = l.Allow(ctx, "key", 1, 100, -1*time.Second)
	if err == nil {
		t.Fatalf("window=-1 should error, got decision %+v", d)
	}
	if d != (ratelimit.Decision{}) {
		t.Fatalf("window=-1 should return zero Decision, got %+v", d)
	}
}

func TestErrorOnNegativeCharge(t *testing.T) {
	l := ratelimit.NewMemory()
	ctx := context.Background()
	d, err := l.Allow(ctx, "key", -1, 100, time.Minute)
	if err == nil {
		t.Fatalf("n=-1 should error, got decision %+v", d)
	}
	if d != (ratelimit.Decision{}) {
		t.Fatalf("n=-1 should return zero Decision, got %+v", d)
	}
}

// The quota stage charges the daily cap to a 24 hour window. Truncating to
// 24 hours lands on UTC midnight, so the window is the calendar day.
func TestADayWindowEndsAtUTCMidnight(t *testing.T) {
	now := time.Date(2026, 10, 7, 23, 30, 0, 0, time.UTC)
	m := ratelimit.NewMemory(ratelimit.WithClock(func() time.Time { return now }))
	d, err := m.Allow(context.Background(), "daily:t", 1, 1, 24*time.Hour)
	if err != nil || !d.Allowed || d.RetryAfter != 30*time.Minute {
		t.Fatalf("day window at 23:30 UTC = %+v, %v; want allowed, 30m to midnight", d, err)
	}
	now = time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	if d, _ := m.Allow(context.Background(), "daily:t", 1, 1, 24*time.Hour); !d.Allowed || d.Count != 1 {
		t.Fatalf("after midnight = %+v; want a fresh day", d)
	}
}

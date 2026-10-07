package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestSweepEndsEndedWindows(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	oldSweepAt := sweepAt
	defer func() { sweepAt = oldSweepAt }()
	sweepAt = 3

	clockNow := now
	l := NewMemory(WithClock(func() time.Time { return clockNow }))
	ctx := context.Background()

	// Create 3 keys at time T, each with a 1-minute window
	l.Allow(ctx, "k1", 1, 100, time.Minute)
	l.Allow(ctx, "k2", 1, 100, time.Minute)
	l.Allow(ctx, "k3", 1, 100, time.Minute)

	// Advance past their window end (61 seconds)
	clockNow = clockNow.Add(61 * time.Second)

	// Insert a 4th key which triggers sweep because len >= sweepAt
	l.Allow(ctx, "k4", 1, 100, time.Minute)

	// Assert that ended windows are gone and only k4 remains
	if len(l.windows) != 1 {
		t.Fatalf("after sweep, expected 1 window, got %d", len(l.windows))
	}
	if _, ok := l.windows["k1"]; ok {
		t.Fatal("k1 should be swept away")
	}
	if _, ok := l.windows["k2"]; ok {
		t.Fatal("k2 should be swept away")
	}
	if _, ok := l.windows["k3"]; ok {
		t.Fatal("k3 should be swept away")
	}
	if _, ok := l.windows["k4"]; !ok {
		t.Fatal("k4 should be present")
	}
}

func TestSweepPreservesLiveWindowsOfOtherKeys(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	oldSweepAt := sweepAt
	defer func() { sweepAt = oldSweepAt }()
	sweepAt = 3

	clockNow := now
	l := NewMemory(WithClock(func() time.Time { return clockNow }))
	ctx := context.Background()

	// Create key1 with a 2-minute window
	l.Allow(ctx, "k1", 1, 100, 2*time.Minute)

	// Create key2 and key3 with 1-minute windows
	l.Allow(ctx, "k2", 1, 100, time.Minute)
	l.Allow(ctx, "k3", 1, 100, time.Minute)

	// Create an ended key that will be swept
	l.Allow(ctx, "ended", 1, 100, time.Minute)

	// Advance past the 1-minute windows but not past the 2-minute window
	clockNow = clockNow.Add(61 * time.Second)

	// Insert a 5th key to trigger sweep
	l.Allow(ctx, "k5", 1, 100, time.Minute)

	// Assert k1 (live) is still present, but ended is gone
	if _, ok := l.windows["k1"]; !ok {
		t.Fatal("k1 with 2-minute window should still be present")
	}
	if _, ok := l.windows["ended"]; ok {
		t.Fatal("ended key should be swept away")
	}
	if _, ok := l.windows["k2"]; ok {
		t.Fatal("k2 should be swept away")
	}
	if _, ok := l.windows["k3"]; ok {
		t.Fatal("k3 should be swept away")
	}

	// Verify k1 is still in its original window with count=2
	d1, _ := l.Allow(ctx, "k1", 1, 100, 2*time.Minute)
	if d1.Count != 2 {
		t.Fatalf("k1 should still have count=2 (1 old + 1 new), got %d", d1.Count)
	}
}

func TestSweepOnlyHappensOncePerMinute(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	oldSweepAt := sweepAt
	defer func() { sweepAt = oldSweepAt }()
	sweepAt = 2

	clockNow := now
	l := NewMemory(WithClock(func() time.Time { return clockNow }))
	ctx := context.Background()

	// At T, create keys a and b with 1s window
	l.Allow(ctx, "a", 1, 100, time.Second)
	l.Allow(ctx, "b", 1, 100, time.Second)

	// At T+2s (both ended), call Allow on key c to trigger sweep
	clockNow = clockNow.Add(2 * time.Second)
	l.Allow(ctx, "c", 1, 100, time.Second)

	// Assert a and b are gone and nextSweep is set
	if _, ok := l.windows["a"]; ok {
		t.Fatal("a should be swept away")
	}
	if _, ok := l.windows["b"]; ok {
		t.Fatal("b should be swept away")
	}
	if _, ok := l.windows["c"]; !ok {
		t.Fatal("c should be present")
	}
	expectedNextSweep := clockNow.Add(time.Minute)
	if l.nextSweep != expectedNextSweep {
		t.Fatalf("nextSweep should be %v, got %v", expectedNextSweep, l.nextSweep)
	}

	// Still at T+2s, insert an ended entry directly and call Allow on key d
	clockNow = clockNow.Add(1 * time.Second) // T+3s, still within the minute
	l.windows["stale"] = &memWindow{
		start: now,
		end:   now.Add(time.Second),
		count: 1,
	}
	l.Allow(ctx, "d", 1, 100, time.Second)

	// Assert "stale" is STILL present because no second sweep ran
	if _, ok := l.windows["stale"]; !ok {
		t.Fatal("stale should still be present (no sweep ran yet)")
	}

	// Advance past nextSweep and call Allow on key e
	clockNow = clockNow.Add(59 * time.Second) // Past nextSweep
	l.Allow(ctx, "e", 1, 100, time.Second)

	// Assert "stale" is now gone because sweep ran
	if _, ok := l.windows["stale"]; ok {
		t.Fatal("stale should be swept away now")
	}
}

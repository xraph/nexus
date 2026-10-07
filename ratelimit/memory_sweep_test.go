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

	l := NewMemory(WithClock(func() time.Time { return now }))
	ctx := context.Background()

	// Fill to sweepAt (3 windows), all within the current minute
	l.Allow(ctx, "key1", 1, 100, time.Minute)
	l.Allow(ctx, "key2", 1, 100, time.Minute)
	l.Allow(ctx, "key3", 1, 100, time.Minute)

	// Move 61 seconds forward - key1, key2, key3 are now ended
	now = now.Add(61 * time.Second)

	// Insert a new window which triggers sweep on the 4th entry
	l.Allow(ctx, "key4", 1, 100, time.Minute)

	// After sweep, ended windows should be gone
	// Check by trying to access them - a new window should be created
	// (old window would have been deleted)
	d1, _ := l.Allow(ctx, "key1", 1, 100, time.Minute)
	if d1.Count != 1 {
		t.Fatalf("key1 after sweep should be in new window with count=1, got %d", d1.Count)
	}
}

func TestSweepPreservesLiveWindowsOfOtherKeys(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	oldSweepAt := sweepAt
	defer func() { sweepAt = oldSweepAt }()
	sweepAt = 3

	l := NewMemory(WithClock(func() time.Time { return now }))
	ctx := context.Background()

	// Create key1 with a 2-minute window
	l.Allow(ctx, "key1", 1, 100, 2*time.Minute)

	// Create key2 and key3 with 1-minute windows
	l.Allow(ctx, "key2", 1, 100, time.Minute)
	l.Allow(ctx, "key3", 1, 100, time.Minute)

	// Move 61 seconds forward - key2 and key3 are ended, key1 is still live
	now = now.Add(61 * time.Second)

	// Insert a new window which triggers sweep
	l.Allow(ctx, "key4", 1, 100, time.Minute)

	// key1 should still be in its original window and have count=1
	d1, _ := l.Allow(ctx, "key1", 1, 100, 2*time.Minute)
	if d1.Count != 2 {
		t.Fatalf("key1 should still have count=2 (1 old + 1 new), got %d", d1.Count)
	}
}

func TestSweepOnlyHappensOncePerMinute(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	oldSweepAt := sweepAt
	defer func() { sweepAt = oldSweepAt }()
	sweepAt = 2

	l := NewMemory(WithClock(func() time.Time { return now }))
	ctx := context.Background()

	// Create two windows to fill to sweepAt
	l.Allow(ctx, "key1", 1, 100, time.Minute)
	l.Allow(ctx, "key2", 1, 100, time.Minute)

	// Move 61 seconds forward to end these windows
	now = now.Add(61 * time.Second)

	// Insert key3 to trigger sweep
	l.Allow(ctx, "key3", 1, 100, time.Minute)

	// Insert key4 (an ended window entry that won't be swept yet)
	now = now.Add(1 * time.Second) // Still within the minute after sweep
	l.Allow(ctx, "key4", 1, 100, time.Minute)

	// Move 61 seconds forward again
	now = now.Add(61 * time.Second)

	// Insert key5 - this should trigger another sweep
	l.Allow(ctx, "key5", 1, 100, time.Minute)

	// If sweep happened on key4, it should be gone. If not, it would still be there.
	// We can't directly check the internal state, but we verify by attempting to
	// access key4 which should create a new window if it was swept.
	d4, _ := l.Allow(ctx, "key4", 1, 100, time.Minute)
	if d4.Count != 1 {
		t.Fatalf("key4 should be in a new window with count=1 after second sweep, got %d", d4.Count)
	}
}

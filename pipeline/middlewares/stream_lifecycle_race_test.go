package middlewares_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/plugin"
	"github.com/xraph/nexus/provider"
)

// countingStream is a safe inner stream for races: every field is atomic or
// guarded, so a report can only come from the lifecycle wrapper.
type countingStream struct {
	closes atomic.Int32
	// gate, when non-nil, holds every Next until it is closed or receives.
	gate chan struct{}
}

func (s *countingStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.closes.Load() > 0 && s.gate == nil {
		return nil, io.EOF
	}
	return &provider.StreamChunk{Delta: provider.Delta{Content: "x"}}, nil
}

func (s *countingStream) Close() error           { s.closes.Add(1); return nil }
func (s *countingStream) Usage() *provider.Usage { return nil }

func newLifecycleStream(t *testing.T, inner provider.Stream, quota middlewares.StreamQuota) provider.Stream {
	t.Helper()
	mw := middlewares.NewStreamLifecycle(plugin.NewRegistry(), middlewares.StreamLifecycleConfig{
		QuotaResolver: func(context.Context) middlewares.StreamQuota { return quota },
	})
	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "m"},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
	}
	resp, err := mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Stream: inner}, nil
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	return resp.Stream
}

// TestStreamLifecycle_NextAndCloseDoNotRace runs a reader in Next while
// another goroutine closes, as a shutdown does. Run it with -race.
func TestStreamLifecycle_NextAndCloseDoNotRace(t *testing.T) {
	t.Parallel()

	inner := &countingStream{}
	stream := newLifecycleStream(t, inner, middlewares.StreamQuota{MaxDuration: time.Minute})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			if _, err := stream.Next(context.Background()); err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		time.Sleep(time.Millisecond)
		_ = stream.Close()
	}()
	wg.Wait()
	_ = stream.Usage()
}

// TestStreamLifecycle_WatchdogAfterCloseDoesNotCloseTwice parks a reader in
// Next, closes the stream, then lets the reader's first chunk arrive. That
// chunk starts the watchdog after Close: it must not start, or must stop at
// once, so the inner stream is closed exactly once.
func TestStreamLifecycle_WatchdogAfterCloseDoesNotCloseTwice(t *testing.T) {
	t.Parallel()

	inner := &countingStream{gate: make(chan struct{})}
	stream := newLifecycleStream(t, inner, middlewares.StreamQuota{MaxDuration: 20 * time.Millisecond})

	done := make(chan error, 1)
	go func() {
		_, err := stream.Next(context.Background())
		done <- err
	}()
	time.Sleep(10 * time.Millisecond) // the reader is parked inside inner.Next

	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	inner.gate <- struct{}{} // the parked Next now returns its first chunk
	if err := <-done; err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("next: %v", err)
	}

	time.Sleep(100 * time.Millisecond) // well past MaxDuration
	if n := inner.closes.Load(); n != 1 {
		t.Fatalf("inner stream closed %d times, want 1", n)
	}

	// A watchdog that started late would have queued its quota error by now.
	// Ungate the inner stream so a Next that reaches it returns at once.
	close(inner.gate)
	if _, err := stream.Next(context.Background()); middlewares.IsQuotaExceeded(err) {
		t.Fatalf("next after close = %v; a watchdog started after Close", err)
	}
}

// TestStreamLifecycle_CloseAfterWatchdogClosesOnce covers the other order:
// the watchdog fires and closes the inner stream, then the caller's own
// deferred Close arrives.
func TestStreamLifecycle_CloseAfterWatchdogClosesOnce(t *testing.T) {
	t.Parallel()

	inner := &countingStream{}
	stream := newLifecycleStream(t, inner, middlewares.StreamQuota{MaxDuration: 20 * time.Millisecond})

	if _, err := stream.Next(context.Background()); err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // the watchdog has fired and closed
	if n := inner.closes.Load(); n != 1 {
		t.Fatalf("watchdog closed the inner stream %d times, want 1", n)
	}
	_ = stream.Close()
	if n := inner.closes.Load(); n != 1 {
		t.Fatalf("inner stream closed %d times after Close, want 1", n)
	}
}

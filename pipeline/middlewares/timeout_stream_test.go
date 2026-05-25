package middlewares_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
)

// TimeoutMiddleware historically wrapped every request with
// context.WithTimeout + defer cancel(). For streaming requests this was
// fatal: `next(ctx)` returns a `provider.Stream` whose lifetime extends
// past the middleware's Process call, but `defer cancel()` would fire
// the instant Process returned — killing the upstream HTTP body and
// surfacing as `context canceled` once the transport's bufio.Reader and
// the provider scanner's buffer drained (typically a few seconds in).
//
// These tests pin the new contract: streams pass through untouched, but
// non-streaming requests still get the wall-clock deadline.

func TestTimeout_StreamSkipsCancellation(t *testing.T) {
	mw := middlewares.NewTimeout(50 * time.Millisecond)

	var observedCtx context.Context
	next := func(ctx context.Context) (*pipeline.Response, error) {
		observedCtx = ctx
		// A real stream-yielding pipeline returns the Stream object here;
		// the test only cares about the lifetime of the ctx it captured.
		return &pipeline.Response{}, nil
	}

	req := &pipeline.Request{Type: pipeline.RequestStream}
	if _, err := mw.Process(context.Background(), req, next); err != nil {
		t.Fatalf("Process returned unexpected error: %v", err)
	}

	if observedCtx == nil {
		t.Fatal("next() was not invoked")
	}

	// The middleware must NOT have wrapped a deadline around the ctx —
	// otherwise the deferred cancel would cancel it the moment Process
	// returned. Wait briefly past the timeout window to be sure.
	select {
	case <-observedCtx.Done():
		t.Fatalf("stream ctx was canceled after Process returned (err=%v); "+
			"streams must not be deadline-wrapped", observedCtx.Err())
	case <-time.After(150 * time.Millisecond):
		// good — ctx still alive past the 50ms middleware timeout.
	}

	if err := observedCtx.Err(); err != nil {
		t.Fatalf("expected ctx to be alive, got err=%v", err)
	}
}

func TestTimeout_NonStreamCancelsOnReturn(t *testing.T) {
	mw := middlewares.NewTimeout(50 * time.Millisecond)

	var observedCtx context.Context
	next := func(ctx context.Context) (*pipeline.Response, error) {
		observedCtx = ctx
		return &pipeline.Response{}, nil
	}

	req := &pipeline.Request{Type: pipeline.RequestCompletion}
	if _, err := mw.Process(context.Background(), req, next); err != nil {
		t.Fatalf("Process returned unexpected error: %v", err)
	}

	if observedCtx == nil {
		t.Fatal("next() was not invoked")
	}

	// For non-stream requests, the defer cancel() runs when Process
	// returns — the captured ctx must already be canceled by now.
	select {
	case <-observedCtx.Done():
		// expected.
	default:
		t.Fatal("non-stream ctx should be canceled after Process returned (defer cancel)")
	}

	if !errors.Is(observedCtx.Err(), context.Canceled) {
		t.Fatalf("expected context.Canceled after Process return, got %v", observedCtx.Err())
	}
}

func TestTimeout_ZeroTimeoutBypassesForBoth(t *testing.T) {
	mw := middlewares.NewTimeout(0)

	var observedCtx context.Context
	next := func(ctx context.Context) (*pipeline.Response, error) {
		observedCtx = ctx
		return &pipeline.Response{}, nil
	}

	for _, reqType := range []pipeline.RequestType{
		pipeline.RequestStream,
		pipeline.RequestCompletion,
		pipeline.RequestEmbedding,
	} {
		observedCtx = nil
		req := &pipeline.Request{Type: reqType}
		if _, err := mw.Process(context.Background(), req, next); err != nil {
			t.Fatalf("[%s] Process returned unexpected error: %v", reqType, err)
		}
		if observedCtx == nil {
			t.Fatalf("[%s] next() was not invoked", reqType)
		}
		// timeout=0 → ctx passes through with no wrapping, so it stays
		// alive even after Process returns.
		if err := observedCtx.Err(); err != nil {
			t.Fatalf("[%s] expected ctx unchanged (no err), got %v", reqType, err)
		}
	}
}

func TestTimeout_NilRequestStillWrapsAsCompletion(t *testing.T) {
	// Defensive: if some caller invokes Process with a nil Request, the
	// stream-bypass branch should NOT fire (we don't know it's a stream).
	// The deadline-wrapping path runs as before.
	mw := middlewares.NewTimeout(50 * time.Millisecond)

	var observedCtx context.Context
	next := func(ctx context.Context) (*pipeline.Response, error) {
		observedCtx = ctx
		return &pipeline.Response{}, nil
	}

	if _, err := mw.Process(context.Background(), nil, next); err != nil {
		t.Fatalf("Process returned unexpected error: %v", err)
	}

	if observedCtx == nil {
		t.Fatal("next() was not invoked")
	}
	if !errors.Is(observedCtx.Err(), context.Canceled) {
		t.Fatalf("expected canceled ctx for nil request (treated as non-stream), got %v", observedCtx.Err())
	}
}

package middlewares_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// TestRetryMiddleware_RetriesStreamInit verifies that transient errors
// raised from CompleteStream (before the first chunk) are retried.
func TestRetryMiddleware_RetriesStreamInit(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "ok"}, FinishReason: "stop"},
	}
	attempts := 0
	mw := middlewares.NewRetry(3, 1*time.Millisecond, 1.0)

	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "m"},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
	}
	resp, err := mw.Process(context.Background(), req, func(_ context.Context) (*pipeline.Response, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("transient")
		}
		return &pipeline.Response{Stream: testutil.NewFakeStream(chunks, nil)}, nil
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if resp.Stream == nil {
		t.Fatal("stream nil after recovery")
	}
}

// TestRetryMiddleware_DoesNotRetryMidStream verifies that once a stream is
// returned, errors surfaced via Stream.Next are not retried — the consumer
// has already started receiving frames and silently retrying would replay
// content.
func TestRetryMiddleware_DoesNotRetryMidStream(t *testing.T) {
	t.Parallel()

	mw := middlewares.NewRetry(3, 1*time.Millisecond, 1.0)
	wraps := 0

	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "m"},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
	}
	resp, err := mw.Process(context.Background(), req, func(_ context.Context) (*pipeline.Response, error) {
		wraps++
		// Stream returns a chunk then errors mid-flight.
		stream := &midStreamErrStream{
			chunks: []*provider.StreamChunk{{Delta: provider.Delta{Content: "partial"}}},
			err:    errors.New("mid-stream rug pull"),
		}
		return &pipeline.Response{Stream: stream}, nil
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if wraps != 1 {
		t.Fatalf("provider invoked %d times — must be exactly 1 (no mid-stream retry)", wraps)
	}

	// Drain to surface the mid-stream error to the caller.
	var firstChunk *provider.StreamChunk
	var midErr error
	for {
		c, e := resp.Stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			t.Fatal("expected mid-stream error, got clean EOF")
		}
		if e != nil {
			midErr = e
			break
		}
		if firstChunk == nil {
			firstChunk = c
		}
	}
	if firstChunk == nil || firstChunk.Delta.Content != "partial" {
		t.Fatalf("first chunk = %+v", firstChunk)
	}
	if midErr.Error() != "mid-stream rug pull" {
		t.Fatalf("mid-stream err = %v", midErr)
	}
}

type midStreamErrStream struct {
	chunks []*provider.StreamChunk
	idx    int
	err    error
}

func (s *midStreamErrStream) Next(_ context.Context) (*provider.StreamChunk, error) {
	if s.idx >= len(s.chunks) {
		return nil, s.err
	}
	c := s.chunks[s.idx]
	s.idx++
	return c, nil
}
func (s *midStreamErrStream) Close() error           { return nil }
func (s *midStreamErrStream) Usage() *provider.Usage { return nil }

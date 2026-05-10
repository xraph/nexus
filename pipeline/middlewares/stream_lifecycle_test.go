package middlewares_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/plugin"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

type captureExt struct {
	mu             sync.Mutex
	started        int
	completed      int
	failed         int
	chunks         int
	chunkBytes     int
	completedFinal *provider.CompletionResponse
	completedModel string
	completedProv  string
	completedID    id.RequestID
	failedErr      error
	completedDur   time.Duration
}

func (e *captureExt) Name() string { return "capture" }

func (e *captureExt) OnStreamStarted(_ context.Context, requestID id.RequestID, model, prov string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.started++
	e.completedID = requestID
	e.completedModel = model
	e.completedProv = prov
	return nil
}

func (e *captureExt) OnChunkReceived(_ context.Context, _ id.RequestID, _ provider.EventKind, byteSize int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.chunks++
	e.chunkBytes += byteSize
	return nil
}

func (e *captureExt) OnStreamCompleted(_ context.Context, _ id.RequestID, model, prov string, elapsed time.Duration, final *provider.CompletionResponse) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.completed++
	e.completedFinal = final
	e.completedModel = model
	e.completedProv = prov
	e.completedDur = elapsed
	return nil
}

func (e *captureExt) OnStreamFailed(_ context.Context, _ id.RequestID, _ string, err error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.failed++
	e.failedErr = err
	return nil
}

var (
	_ plugin.Extension       = (*captureExt)(nil)
	_ plugin.StreamStarted   = (*captureExt)(nil)
	_ plugin.ChunkReceived   = (*captureExt)(nil)
	_ plugin.StreamCompleted = (*captureExt)(nil)
	_ plugin.StreamFailed    = (*captureExt)(nil)
)

func runStreamLifecycle(t *testing.T, stream provider.Stream) (*captureExt, *pipeline.Response, error) {
	t.Helper()
	ext := &captureExt{}
	registry := plugin.NewRegistry()
	registry.Register(ext)

	mw := middlewares.NewStreamLifecycle(registry, middlewares.StreamLifecycleConfig{})

	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "test-model"},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
	}
	next := func(_ context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Stream: stream}, nil
	}
	resp, err := mw.Process(context.Background(), req, next)
	return ext, resp, err
}

func TestStreamLifecycle_FiresStartChunksAndCompleted(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{Provider: "openai", Model: "gpt-4o", Delta: provider.Delta{Role: "assistant", Content: "Hi"}},
		{Delta: provider.Delta{Content: "!"}, FinishReason: "stop"},
	}
	stream := testutil.NewFakeStream(chunks, &provider.Usage{TotalTokens: 5})

	ext, resp, err := runStreamLifecycle(t, stream)
	if err != nil || resp == nil || resp.Stream == nil {
		t.Fatalf("middleware did not pass stream through: err=%v resp=%+v", err, resp)
	}

	// Drain via the wrapper.
	for {
		_, e := resp.Stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("unexpected stream error: %v", e)
		}
	}
	_ = resp.Stream.Close()

	if ext.started != 1 {
		t.Fatalf("started fired %d times, want 1", ext.started)
	}
	if ext.completed != 1 {
		t.Fatalf("completed fired %d times, want 1", ext.completed)
	}
	if ext.failed != 0 {
		t.Fatalf("failed fired %d times, want 0", ext.failed)
	}
	if ext.chunks != 2 {
		t.Fatalf("chunks counted = %d, want 2", ext.chunks)
	}
	if ext.completedFinal == nil {
		t.Fatal("completed missed final response")
	}
	if got := ext.completedFinal.Choices[0].Message.Content; got != "Hi!" {
		t.Fatalf("final content = %q, want Hi!", got)
	}
	if ext.completedFinal.Usage.TotalTokens != 5 {
		t.Fatalf("final usage = %+v", ext.completedFinal.Usage)
	}
}

func TestStreamLifecycle_FiresFailedOnError(t *testing.T) {
	t.Parallel()

	stream := &errorStream{err: errors.New("upstream boom")}

	ext, resp, err := runStreamLifecycle(t, stream)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if resp == nil || resp.Stream == nil {
		t.Fatal("resp/stream nil")
	}
	if _, e := resp.Stream.Next(context.Background()); e == nil {
		t.Fatal("expected stream error")
	}

	if ext.failed != 1 {
		t.Fatalf("failed fired %d times, want 1", ext.failed)
	}
	if ext.completed != 0 {
		t.Fatalf("completed fired %d times, want 0", ext.completed)
	}
}

type errorStream struct {
	err error
}

func (s *errorStream) Next(_ context.Context) (*provider.StreamChunk, error) { return nil, s.err }
func (s *errorStream) Close() error                                          { return nil }
func (s *errorStream) Usage() *provider.Usage                                { return nil }

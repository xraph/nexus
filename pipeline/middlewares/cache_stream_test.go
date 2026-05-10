package middlewares_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func TestCacheMiddleware_StreamRecordAndReplay(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{Provider: "test", Model: "m", Delta: provider.Delta{Content: "hello"}},
		{Delta: provider.Delta{Content: " world"}, FinishReason: "stop"},
		{Kind: provider.EventUsage, Usage: &provider.Usage{TotalTokens: 7}},
	}

	streamCache := stores.NewMemoryStream()
	mw := middlewares.NewCache(nil).WithStreamCache(streamCache, cache.StreamCacheOptions{Mode: cache.ReplayBurst})

	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "m", Stream: true, Messages: []provider.Message{{Role: "user", Content: "hi"}}},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
	}

	// First call: cache miss, record frames.
	first := testutil.NewFakeStream(chunks, nil)
	resp, err := mw.Process(context.Background(), req, func(_ context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Stream: first}, nil
	})
	if err != nil {
		t.Fatalf("first process: %v", err)
	}
	if resp.Stream == nil {
		t.Fatal("first call: stream nil")
	}

	for {
		_, e := resp.Stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("first drain: %v", e)
		}
	}
	_ = resp.Stream.Close()

	// Second call: cache hit — different upstream "stream" should not be touched.
	upstreamCalled := false
	resp2, err := mw.Process(context.Background(), req, func(_ context.Context) (*pipeline.Response, error) {
		upstreamCalled = true
		return &pipeline.Response{Stream: testutil.NewFakeStream(nil, nil)}, nil
	})
	if err != nil {
		t.Fatalf("second process: %v", err)
	}
	if upstreamCalled {
		t.Fatal("expected cache hit — upstream should not have been invoked")
	}

	var got string
	for {
		c, e := resp2.Stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("replay: %v", e)
		}
		got += c.Delta.Content
	}
	if got != "hello world" {
		t.Fatalf("replayed content = %q", got)
	}
	if u := resp2.Stream.Usage(); u == nil || u.TotalTokens != 7 {
		t.Fatalf("usage on replay = %+v", u)
	}
}

func TestCacheMiddleware_StreamMaxFramesAbandonsRecording(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "a"}},
		{Delta: provider.Delta{Content: "b"}},
		{Delta: provider.Delta{Content: "c"}, FinishReason: "stop"},
	}

	sc := stores.NewMemoryStream()
	mw := middlewares.NewCache(nil).WithStreamCache(sc, cache.StreamCacheOptions{MaxFrames: 1})

	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "m", Stream: true, Messages: []provider.Message{{Role: "user", Content: "hi"}}},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
	}
	resp, err := mw.Process(context.Background(), req, func(_ context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Stream: testutil.NewFakeStream(chunks, nil)}, nil
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	for {
		_, e := resp.Stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("drain: %v", e)
		}
	}
	_ = resp.Stream.Close()

	frames, _ := sc.GetStream(context.Background(), cache.StreamKey(req.Completion))
	if frames != nil {
		t.Fatalf("expected recording abandoned, got %d frames", len(frames))
	}
}

func TestReplayMode_PacedSleepsBetweenFrames(t *testing.T) {
	t.Parallel()

	frames := []cache.StreamFrame{
		{Chunk: &provider.StreamChunk{Delta: provider.Delta{Content: "a"}}, OffsetMs: 0},
		{Chunk: &provider.StreamChunk{Delta: provider.Delta{Content: "b"}}, OffsetMs: 50},
	}
	sc := stores.NewMemoryStream()
	if err := sc.SetStream(context.Background(), "k", frames, 0); err != nil {
		t.Fatalf("set: %v", err)
	}

	mw := middlewares.NewCache(nil).WithStreamCache(sc, cache.StreamCacheOptions{Mode: cache.ReplayPaced})

	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "m", Stream: true, Messages: []provider.Message{{Role: "user", Content: "hi"}}},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
	}
	// Inject frames under the canonical key.
	canonicalKey := cache.StreamKey(req.Completion)
	_ = sc.SetStream(context.Background(), canonicalKey, frames, 0)

	start := time.Now()
	resp, err := mw.Process(context.Background(), req, func(_ context.Context) (*pipeline.Response, error) {
		t.Fatal("upstream invoked despite cache hit")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	for {
		_, e := resp.Stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("replay: %v", e)
		}
	}
	elapsed := time.Since(start)
	if elapsed < 40*time.Millisecond {
		t.Fatalf("paced replay too fast: %v", elapsed)
	}
}

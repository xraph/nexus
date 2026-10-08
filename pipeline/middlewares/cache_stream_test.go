package middlewares_test

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/tenant"
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
	if req.State[pipeline.StateCacheHit] != true {
		t.Fatalf("a stream replay must set State[StateCacheHit], got %v", req.State[pipeline.StateCacheHit])
	}
}

func TestCacheMiddleware_HitSetsCacheHitFlag(t *testing.T) {
	t.Parallel()

	mw := middlewares.NewCache(cache.NewService(stores.NewMemory()))
	newReq := func() *pipeline.Request {
		return &pipeline.Request{
			Completion: &provider.CompletionRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "hi"}}},
			Type:       pipeline.RequestCompletion,
			State:      map[string]any{},
		}
	}
	next := func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Completion: &provider.CompletionResponse{Model: "m"}}, nil
	}

	first := newReq()
	if _, err := mw.Process(context.Background(), first, next); err != nil {
		t.Fatalf("first: %v", err)
	}
	if first.State[pipeline.StateCacheHit] == true {
		t.Fatal("a miss must not set State[StateCacheHit]")
	}

	second := newReq()
	resp, err := mw.Process(context.Background(), second, func(context.Context) (*pipeline.Response, error) {
		t.Fatal("expected cache hit, upstream was invoked")
		return nil, nil //nolint:nilnil // unreachable
	})
	if err != nil || resp == nil || resp.Completion == nil || !resp.Completion.Cached {
		t.Fatalf("second = %+v, %v", resp, err)
	}
	if second.State[pipeline.StateCacheHit] != true {
		t.Fatalf("a hit must set State[StateCacheHit], got %v", second.State[pipeline.StateCacheHit])
	}
}

func TestCacheMiddleware_NeverHandsOutTheStoredResponse(t *testing.T) {
	t.Parallel()

	mw := middlewares.NewCache(cache.NewService(stores.NewMemory()))
	newReq := func() *pipeline.Request {
		return &pipeline.Request{
			Completion: &provider.CompletionRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "hi"}}},
			Type:       pipeline.RequestCompletion,
			State:      map[string]any{},
		}
	}
	miss, err := mw.Process(context.Background(), newReq(), func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Completion: &provider.CompletionResponse{Model: "m",
			Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "original"}}}}}, nil
	})
	if err != nil {
		t.Fatalf("miss: %v", err)
	}
	// An output guard redacts the response the miss is still carrying.
	miss.Completion.Choices[0].Message = provider.Message{Role: "assistant", Content: "redacted"}

	hit, err := mw.Process(context.Background(), newReq(), func(context.Context) (*pipeline.Response, error) {
		t.Fatal("expected a cache hit")
		return nil, nil //nolint:nilnil // unreachable
	})
	if err != nil || hit == nil || hit.Completion == nil {
		t.Fatalf("hit = %+v, %v", hit, err)
	}
	if miss.Completion.Cached {
		t.Fatal("the hit marked the miss's response as cached")
	}
	if got := hit.Completion.Choices[0].Message.Content; got != "original" {
		t.Fatalf("the hit served %v: the miss's redaction reached the stored entry", got)
	}
	// And a stage rewriting the hit must not reach the next hit.
	hit.Completion.Choices[0].Message = provider.Message{Role: "assistant", Content: "changed"}
	again, _ := mw.Process(context.Background(), newReq(), func(context.Context) (*pipeline.Response, error) {
		t.Fatal("expected a cache hit")
		return nil, nil //nolint:nilnil // unreachable
	})
	if got := again.Completion.Choices[0].Message.Content; got != "original" {
		t.Fatalf("the second hit served %v", got)
	}
}

// callTerminal ends a test pipeline and counts the provider calls.
type callTerminal struct{ calls *atomic.Int64 }

func (callTerminal) Name() string  { return "provider_call" }
func (callTerminal) Priority() int { return 350 }
func (callTerminal) Terminal()     {}
func (c callTerminal) Process(context.Context, *pipeline.Request, pipeline.NextFunc) (*pipeline.Response, error) {
	c.calls.Add(1)
	return &pipeline.Response{Completion: &provider.CompletionResponse{Model: "m"}}, nil
}

func TestCacheKeysAContextOnlyTenantWithoutTheIdentityStage(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	p, err := pipeline.NewBuilder().
		Use(middlewares.NewCache(cache.NewService(stores.NewMemory())), callTerminal{&calls}).
		Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	a, b := id.NewTenantID().String(), id.NewTenantID().String()
	run := func(ctxTenant, reqTenant string) {
		t.Helper()
		ctx := pipeline.WithTenantID(context.Background(), ctxTenant)
		req := &provider.CompletionRequest{Model: "m", TenantID: reqTenant, Messages: []provider.Message{{Role: "user", Content: "same"}}}
		if _, err := p.Execute(ctx, req); err != nil {
			t.Fatalf("execute: %v", err)
		}
	}

	// Two tenants named only in the context must not share an entry.
	run(a, "")
	run(b, "")
	if calls.Load() != 2 {
		t.Fatalf("provider calls = %d; tenant B was served tenant A's cached completion", calls.Load())
	}
	run(a, "") // and each still hits its own
	if calls.Load() != 2 {
		t.Fatalf("provider calls = %d; tenant A's repeat should hit the cache", calls.Load())
	}

	// A request whose context and fields disagree bypasses the cache: it
	// reads neither tenant's entry and writes none.
	run(a, b)
	run(a, b)
	if calls.Load() != 4 {
		t.Fatalf("provider calls = %d; a request naming two tenants must bypass the cache", calls.Load())
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

func TestCacheMiddleware_TenantCacheSwitch(t *testing.T) {
	t.Parallel()

	newReq := func() *pipeline.Request {
		return &pipeline.Request{
			Completion: &provider.CompletionRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "hi"}}},
			Type:       pipeline.RequestCompletion,
			State:      map[string]any{},
		}
	}
	run := func(t *testing.T, cacheEnabled *bool) (upstream int, secondHit bool) {
		t.Helper()
		mw := middlewares.NewCache(cache.NewService(stores.NewMemory()))
		ctx := middlewares.WithTenantForTest(context.Background(), &tenant.Tenant{Config: tenant.Config{CacheEnabled: cacheEnabled}})
		next := func(context.Context) (*pipeline.Response, error) {
			upstream++
			return &pipeline.Response{Completion: &provider.CompletionResponse{Model: "m"}}, nil
		}
		for i := 0; i < 2; i++ {
			req := newReq()
			if _, err := mw.Process(ctx, req, next); err != nil {
				t.Fatal(err)
			}
			secondHit = req.State[pipeline.StateCacheHit] == true
		}
		return upstream, secondHit
	}

	off := false
	if upstream, hit := run(t, &off); upstream != 2 || hit {
		t.Fatalf("cache off: upstream calls %d, second hit %v; want both requests to reach next and no hit", upstream, hit)
	}
	if upstream, hit := run(t, nil); upstream != 1 || !hit {
		t.Fatalf("cache unset: upstream calls %d, second hit %v; want the second request served from the cache", upstream, hit)
	}
	on := true
	if upstream, hit := run(t, &on); upstream != 1 || !hit {
		t.Fatalf("cache on: upstream calls %d, second hit %v; want the second request served from the cache", upstream, hit)
	}
}

func TestCacheMiddleware_TenantCacheSwitchOffNeitherReplaysNorRecordsAStream(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{Provider: "test", Model: "m", Delta: provider.Delta{Content: "hello"}, FinishReason: "stop"},
	}
	mw := middlewares.NewCache(nil).WithStreamCache(stores.NewMemoryStream(), cache.StreamCacheOptions{Mode: cache.ReplayBurst})
	newReq := func() *pipeline.Request {
		return &pipeline.Request{
			Completion: &provider.CompletionRequest{Model: "m", Stream: true, Messages: []provider.Message{{Role: "user", Content: "hi"}}},
			Type:       pipeline.RequestStream,
			State:      map[string]any{},
		}
	}
	drain := func(s provider.Stream) {
		t.Helper()
		for {
			if _, err := s.Next(context.Background()); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("drain: %v", err)
			}
		}
		_ = s.Close()
	}

	off := false
	offCtx := middlewares.WithTenantForTest(context.Background(), &tenant.Tenant{Config: tenant.Config{CacheEnabled: &off}})
	for i := 0; i < 2; i++ {
		upstream := testutil.NewFakeStream(chunks, nil)
		req := newReq()
		resp, err := mw.Process(offCtx, req, func(context.Context) (*pipeline.Response, error) {
			return &pipeline.Response{Stream: upstream}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Stream != upstream {
			t.Fatalf("request %d: the stream was wrapped for recording; want the upstream stream handed through", i+1)
		}
		if req.State[pipeline.StateCacheHit] == true {
			t.Fatalf("request %d replayed a cached stream", i+1)
		}
		drain(resp.Stream)
	}

	// Nothing was recorded: a tenant with the cache on still misses.
	called := false
	resp, err := mw.Process(context.Background(), newReq(), func(context.Context) (*pipeline.Response, error) {
		called = true
		return &pipeline.Response{Stream: testutil.NewFakeStream(chunks, nil)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	drain(resp.Stream)
	if !called {
		t.Fatal("a stream was recorded for a tenant whose cache is off")
	}
}

package middlewares

import (
	"context"
	"errors"
	"io"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

// CacheMiddleware checks the cache before calling the provider and stores
// successful responses. It supports two cache tiers:
//
//   - Cache (provider.CompletionResponse): used for non-streaming requests.
//   - StreamCache (ordered StreamFrames): used for streaming requests; on
//     miss, the inner stream is wrapped with a recorder that buffers frames
//     and writes them to the cache on Close. On hit, the cached frames are
//     replayed via a synthesized provider.Stream.
//
// Streaming caching is opt-in: pass a StreamCache via WithStreamCache.
type CacheMiddleware struct {
	cache       cache.Service
	streamCache cache.StreamCache
	streamOpts  cache.StreamCacheOptions
}

// NewCache creates a caching middleware backed only by the
// CompletionResponse cache tier.
//
// Tenant isolation depends on the cache key naming the tenant: the request's
// TenantID, or the context's (pipeline.WithTenantID) when the request names
// none. When both name a tenant and they differ, the request bypasses the
// cache, reading and writing nothing.
func NewCache(c cache.Service) *CacheMiddleware {
	return &CacheMiddleware{cache: c}
}

// WithStreamCache enables stream record-and-replay backed by sc, applying
// the given options. Passing a zero-value opts struct uses ReplayBurst,
// no caps, and no TTL.
//
// Validation: when Mode is ReplayFastForward, FFDivisor must be > 0; a
// non-positive divisor silently coerces to a default of 4× speedup so
// callers don't accidentally lock themselves into ReplayPaced behaviour.
func (m *CacheMiddleware) WithStreamCache(sc cache.StreamCache, opts cache.StreamCacheOptions) *CacheMiddleware {
	if opts.Mode == cache.ReplayFastForward && opts.FFDivisor <= 0 {
		opts.FFDivisor = 4
	}
	m.streamCache = sc
	m.streamOpts = opts
	return m
}

func (m *CacheMiddleware) Name() string  { return "cache" }
func (m *CacheMiddleware) Priority() int { return 280 } // After transforms, before routing

func (m *CacheMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if req.Completion == nil {
		return next(ctx)
	}

	keyReq, ok := tenantScoped(ctx, req.Completion)
	if !ok {
		return next(ctx)
	}

	if req.Type == pipeline.RequestStream {
		return m.handleStream(ctx, req, keyReq, next)
	}

	if m.cache == nil {
		return next(ctx)
	}

	// Generate cache key
	key := cache.Key(keyReq)

	// Check cache
	// The store may hand every caller the same object, so a hit gets its own
	// copy before anything marks or redacts it.
	cached, err := m.cache.Get(ctx, key)
	if err == nil && cached != nil {
		hit := copyCompletion(cached)
		hit.Cached = true
		req.State[pipeline.StateCacheHit] = true
		return &pipeline.Response{Completion: hit}, nil
	}

	// Cache miss — continue pipeline
	resp, err := next(ctx)
	if err != nil {
		return resp, err
	}

	// Store a copy of the successful response, so the stages this response
	// still passes through (an output guard's redaction, output transforms)
	// never write into the stored entry.
	if resp != nil && resp.Completion != nil {
		_ = m.cache.Set(ctx, key, copyCompletion(resp.Completion)) //nolint:errcheck // best-effort cache store
	}

	return resp, nil
}

// copyCompletion returns a copy of r that later stages can change without
// touching r. Choices and State are copied too: output guards and output
// transforms replace a choice's message or content in place, and stages add
// to State. A message's content is replaced, never edited, so copying the
// choices by value is enough.
func copyCompletion(r *provider.CompletionResponse) *provider.CompletionResponse {
	cp := *r
	cp.Choices = slices.Clone(r.Choices)
	cp.State = maps.Clone(r.State)
	return &cp
}

// tenantScoped returns the request the cache key is computed from: the
// request itself when it names a tenant, or a copy carrying the context's
// tenant when only the context names one. Without this, a pipeline with no
// identity stage would key two context-only tenants to the same entry. ok is
// false when the request and the context name different tenants: such a
// request bypasses the cache.
func tenantScoped(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionRequest, bool) {
	fromCtx := pipeline.TenantID(ctx)
	switch {
	case fromCtx == "" || fromCtx == req.TenantID:
		return req, true
	case req.TenantID != "":
		return nil, false
	}
	keyReq := *req
	keyReq.TenantID = fromCtx
	return &keyReq, true
}

func (m *CacheMiddleware) handleStream(ctx context.Context, req *pipeline.Request, keyReq *provider.CompletionRequest, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.streamCache == nil {
		// No stream cache configured: pass through.
		return next(ctx)
	}

	key := cache.StreamKey(keyReq)

	// Cache hit: replay stored frames as a synthesized stream.
	if frames, err := m.streamCache.GetStream(ctx, key); err == nil && len(frames) > 0 {
		req.State[pipeline.StateCacheHit] = true
		return &pipeline.Response{Stream: newReplayStream(frames, m.streamOpts)}, nil
	}

	// Cache miss: continue pipeline, then wrap the resulting stream with a
	// recorder that flushes to the cache on Close.
	resp, err := next(ctx)
	if err != nil || resp == nil || resp.Stream == nil {
		return resp, err
	}

	resp.Stream = &recordingStream{
		inner:   resp.Stream,
		cache:   m.streamCache,
		key:     key,
		opts:    m.streamOpts,
		startAt: time.Now(),
	}
	return resp, nil
}

// recordingStream captures every chunk into an in-memory buffer, with the
// arrival offset, and writes the buffer to the StreamCache on Close. Caps
// from StreamCacheOptions abandon the recording silently if exceeded.
type recordingStream struct {
	inner   provider.Stream
	cache   cache.StreamCache
	key     string
	opts    cache.StreamCacheOptions
	startAt time.Time

	mu        sync.Mutex
	frames    []cache.StreamFrame
	bytesUsed int
	abandoned bool
	closed    bool
}

func (s *recordingStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	chunk, err := s.inner.Next(ctx)
	if chunk != nil {
		s.recordFrame(chunk)
	}
	return chunk, err
}

func (s *recordingStream) recordFrame(c *provider.StreamChunk) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.abandoned {
		return
	}
	if s.opts.MaxFrames > 0 && len(s.frames) >= s.opts.MaxFrames {
		s.abandoned = true
		s.frames = nil
		return
	}
	size := approxFrameSize(c)
	if s.opts.MaxBytes > 0 && s.bytesUsed+size > s.opts.MaxBytes {
		s.abandoned = true
		s.frames = nil
		return
	}
	s.bytesUsed += size
	cp := *c
	s.frames = append(s.frames, cache.StreamFrame{
		Chunk:    &cp,
		OffsetMs: time.Since(s.startAt).Milliseconds(),
	})
}

func (s *recordingStream) Close() error {
	s.mu.Lock()
	already := s.closed
	s.closed = true
	frames := s.frames
	abandoned := s.abandoned
	s.mu.Unlock()

	closeErr := s.inner.Close()
	if already || abandoned || len(frames) == 0 {
		return closeErr
	}

	ttl := s.opts.TTL
	_ = s.cache.SetStream(context.Background(), s.key, frames, ttl) //nolint:errcheck // best-effort
	return closeErr
}

func (s *recordingStream) Usage() *provider.Usage { return s.inner.Usage() }

// replayStream re-emits frames previously captured by recordingStream. It
// honours the configured ReplayMode for cadence; usage is taken from the
// last EventUsage frame found, or the final FinishReason chunk.
type replayStream struct {
	frames []cache.StreamFrame
	idx    int
	opts   cache.StreamCacheOptions
	usage  *provider.Usage
	last   time.Time
}

func newReplayStream(frames []cache.StreamFrame, opts cache.StreamCacheOptions) *replayStream {
	rs := &replayStream{frames: frames, opts: opts, last: time.Now()}
	for _, f := range frames {
		if f.Chunk != nil && f.Chunk.Usage != nil {
			rs.usage = f.Chunk.Usage
		}
	}
	return rs
}

func (s *replayStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s.idx >= len(s.frames) {
		return nil, io.EOF
	}
	frame := s.frames[s.idx]
	if err := s.waitForCadence(ctx, frame); err != nil {
		return nil, err
	}
	s.idx++
	chunk := frame.Chunk
	if chunk == nil {
		return nil, errors.New("cache: replay frame has nil chunk")
	}
	return chunk, nil
}

func (s *replayStream) waitForCadence(ctx context.Context, frame cache.StreamFrame) error {
	switch s.opts.Mode {
	case cache.ReplayBurst:
		return nil
	case cache.ReplayPaced, cache.ReplayFastForward:
		if s.idx == 0 {
			return nil
		}
		prevOffset := s.frames[s.idx-1].OffsetMs
		gap := frame.OffsetMs - prevOffset
		if gap <= 0 {
			return nil
		}
		divisor := s.opts.FFDivisor
		if s.opts.Mode == cache.ReplayPaced || divisor <= 0 {
			divisor = 1
		}
		wait := time.Duration(float64(gap)/divisor) * time.Millisecond
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *replayStream) Close() error           { return nil }
func (s *replayStream) Usage() *provider.Usage { return s.usage }

// approxFrameSize is a cheap estimate of a chunk's payload bytes, used for
// the MaxBytes cap. We don't serialize JSON here — that's the consumer's
// problem.
func approxFrameSize(c *provider.StreamChunk) int {
	if c == nil {
		return 0
	}
	n := len(c.Delta.Content) + len(c.Delta.Reasoning) + len(c.Delta.Refusal) + len(c.Delta.Transcript)
	for i := range c.Delta.ToolCalls {
		n += len(c.Delta.ToolCalls[i].Function.Name) + len(c.Delta.ToolCalls[i].Function.Arguments)
	}
	if c.Delta.Audio != nil {
		n += len(c.Delta.Audio.Data) + len(c.Delta.Audio.Transcript)
	}
	if c.Delta.Image != nil {
		n += len(c.Delta.Image.Data)
	}
	return n
}

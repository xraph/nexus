package middlewares

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/plugin"
	"github.com/xraph/nexus/provider"
)

// StreamLifecycleConfig tunes hook emission to balance visibility against
// hot-path overhead. Defaults: hooks fire every chunk when at least one
// extension implements ChunkReceived; ignored entirely when no one listens.
type StreamLifecycleConfig struct {
	// EmitEveryNChunks throttles per-chunk hook firing. 0 or 1 = every chunk;
	// higher values cut overhead at the cost of granularity. Always fires on
	// the first chunk and on the final chunk regardless of N.
	EmitEveryNChunks int

	// QuotaResolver, when non-nil, is invoked on stream start to discover
	// per-request stream limits — typically derived from the tenant context.
	// Returning a zero StreamQuota disables enforcement for that request.
	//
	// The resolver is decoupled from the tenant package so this middleware
	// stays agnostic to where quotas come from (tenant config, API key
	// metadata, request-scoped overrides, …).
	QuotaResolver func(ctx context.Context) StreamQuota
}

// StreamQuota describes per-request streaming limits enforced by the
// lifecycle middleware. Zero values disable the corresponding check.
type StreamQuota struct {
	// MaxDuration aborts the stream when the elapsed wall-clock time since
	// the first chunk exceeds this value.
	MaxDuration time.Duration

	// MaxTokens aborts the stream when the running output-token count
	// (sourced from EventUsage frames or per-chunk Usage hints) exceeds
	// this value. Best-effort: only meaningful when the upstream provider
	// emits incremental token counts.
	MaxTokens int
}

// StreamLifecycleMiddleware fires plugin hooks across the lifecycle of a
// streamed response and synthesises a merged CompletionResponse for hooks /
// usage / audit downstream.
//
// Position: priority 545 — after all transforms / cache / retry / provider
// call, but before UsageMiddleware (550) so the wrapper is the outermost
// envelope when usage tries to record.
type StreamLifecycleMiddleware struct {
	registry *plugin.Registry
	cfg      StreamLifecycleConfig
}

// NewStreamLifecycle returns a middleware that wires plugin hooks onto
// streaming responses. Pass the gateway's plugin registry; nil disables.
func NewStreamLifecycle(r *plugin.Registry, cfg StreamLifecycleConfig) *StreamLifecycleMiddleware {
	return &StreamLifecycleMiddleware{registry: r, cfg: cfg}
}

func (m *StreamLifecycleMiddleware) Name() string  { return "stream_lifecycle" }
func (m *StreamLifecycleMiddleware) Priority() int { return 545 }

// StateKeyStreamFinalResponse is the pipeline.Request.State key under which
// the merged CompletionResponse is published once the stream completes.
// Downstream middleware (UsageMiddleware) reads it to capture token totals
// without re-buffering chunks.
const StateKeyStreamFinalResponse = "stream.final_response"

func (m *StreamLifecycleMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.registry == nil || req.Type != pipeline.RequestStream {
		return next(ctx)
	}

	requestID := parseRequestID(pipeline.RequestID(ctx))
	model := ""
	if req.Completion != nil {
		model = req.Completion.Model
	}

	resp, err := next(ctx)
	if err != nil {
		m.registry.EmitStreamFailed(ctx, requestID, model, err)
		return resp, err
	}
	if resp == nil || resp.Stream == nil {
		return resp, nil
	}

	providerName := pipeline.ProviderName(ctx)

	var quota StreamQuota
	if m.cfg.QuotaResolver != nil {
		quota = m.cfg.QuotaResolver(ctx)
	}

	ls := &lifecycleStream{
		inner:        resp.Stream,
		ctx:          ctx,
		registry:     m.registry,
		requestID:    requestID,
		model:        model,
		providerName: providerName,
		startedAt:    time.Now(),
		emitEvery:    m.cfg.EmitEveryNChunks,
		req:          req,
		quota:        quota,
	}
	if quota.MaxDuration > 0 || quota.MaxTokens > 0 {
		ls.quotaErr = make(chan error, 1)
	}
	resp.Stream = ls
	return resp, nil
}

// lifecycleStream wraps a provider.Stream to fire plugin hooks and capture
// the merged final response. Methods are not concurrency-safe by themselves
// — same as the underlying Stream contract, where Next must not be called
// concurrently with itself.
type lifecycleStream struct {
	inner        provider.Stream
	ctx          context.Context
	registry     *plugin.Registry
	requestID    id.RequestID
	model        string
	providerName string

	startedAt time.Time
	emitEvery int

	req *pipeline.Request

	once         sync.Once
	startedFired bool
	chunkCount   int
	closed       bool
	finalResp    *provider.CompletionResponse

	// accumulator merges deltas as they pass through. Built lazily so the
	// hot path stays cheap when no completion-style hook is registered.
	acc *provider.Accumulator

	// Quota enforcement state. quotaErr is non-nil when a quota is active;
	// when the watchdog or per-chunk check trips, it sends the violation
	// error and Next returns it on the next invocation.
	quota          StreamQuota
	quotaErr       chan error
	quotaTokenSeen int
	watchdogStop   chan struct{}
}

func (s *lifecycleStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	// Quota violation already detected — surface it.
	if s.quotaErr != nil {
		select {
		case qe := <-s.quotaErr:
			s.finishOnce(ctx, qe)
			return nil, qe
		default:
		}
	}

	chunk, err := s.inner.Next(ctx)

	if errors.Is(err, io.EOF) {
		s.finishOnce(ctx, nil)
		return nil, err
	}
	if err != nil {
		s.finishOnce(ctx, err)
		return nil, err
	}
	if chunk == nil {
		return nil, nil
	}

	if !s.startedFired {
		s.startedFired = true
		s.registry.EmitStreamStarted(s.ctx, s.requestID, s.model, s.providerName)
		s.startWatchdog()
	}

	s.chunkCount++
	if s.shouldEmitChunk() {
		s.registry.EmitChunkReceived(s.ctx, s.requestID, chunk.Kind, estimateChunkSize(chunk))
	}

	if s.acc == nil {
		s.acc = provider.NewAccumulator()
	}
	s.acc.Add(chunk)

	if s.quota.MaxTokens > 0 && chunk.Usage != nil {
		s.quotaTokenSeen = chunk.Usage.CompletionTokens
		if s.quotaTokenSeen > s.quota.MaxTokens {
			err := errQuotaExceeded("output_tokens")
			s.finishOnce(ctx, err)
			return nil, err
		}
	}

	return chunk, nil
}

// startWatchdog kicks off a goroutine that fires when MaxDuration elapses,
// closing the upstream stream so the next Next() call surfaces an error.
func (s *lifecycleStream) startWatchdog() {
	if s.quotaErr == nil || s.quota.MaxDuration <= 0 {
		return
	}
	s.watchdogStop = make(chan struct{})
	go func() {
		t := time.NewTimer(s.quota.MaxDuration)
		defer t.Stop()
		select {
		case <-t.C:
			select {
			case s.quotaErr <- errQuotaExceeded("duration"):
			default:
			}
			_ = s.inner.Close()
		case <-s.watchdogStop:
		}
	}()
}

// QuotaError is the typed sentinel emitted when a stream is canceled by
// the lifecycle middleware's quota watchdog. Use IsQuotaExceeded to detect.
type QuotaError struct{ What string }

func (e *QuotaError) Error() string { return "nexus: stream quota exceeded: " + e.What }

func errQuotaExceeded(what string) error { return &QuotaError{What: what} }

// IsQuotaExceeded reports whether err is a stream-quota violation.
func IsQuotaExceeded(err error) bool {
	var qe *QuotaError
	return errors.As(err, &qe)
}

func (s *lifecycleStream) shouldEmitChunk() bool {
	if !s.registry.HasChunkReceived() {
		return false
	}
	if s.emitEvery <= 1 {
		return true
	}
	return s.chunkCount == 1 || (s.chunkCount%s.emitEvery == 0)
}

func (s *lifecycleStream) Close() error {
	if s.watchdogStop != nil {
		select {
		case <-s.watchdogStop:
		default:
			close(s.watchdogStop)
		}
	}
	s.finishOnce(s.ctx, nil)
	return s.inner.Close()
}

func (s *lifecycleStream) Usage() *provider.Usage { return s.inner.Usage() }

func (s *lifecycleStream) finishOnce(ctx context.Context, streamErr error) {
	s.once.Do(func() {
		elapsed := time.Since(s.startedAt)
		if streamErr != nil {
			s.registry.EmitStreamFailed(ctx, s.requestID, s.model, streamErr)
			return
		}
		final := s.buildFinal()
		s.finalResp = final
		if s.req != nil {
			if s.req.State == nil {
				s.req.State = make(map[string]any)
			}
			s.req.State[StateKeyStreamFinalResponse] = final
		}
		s.registry.EmitStreamCompleted(ctx, s.requestID, s.model, s.providerName, elapsed, final)
	})
	s.closed = true
}

func (s *lifecycleStream) buildFinal() *provider.CompletionResponse {
	if s.acc == nil {
		s.acc = provider.NewAccumulator()
	}
	resp := s.acc.Finalize(s.inner.Usage)
	if resp.Provider == "" {
		resp.Provider = s.providerName
	}
	if resp.Model == "" {
		resp.Model = s.model
	}
	resp.Latency = time.Since(s.startedAt)
	return resp
}

// estimateChunkSize is a cheap byte-count proxy: we add the obvious string
// fields without serializing JSON. Good enough for "approximate emitted
// payload size" telemetry.
func estimateChunkSize(c *provider.StreamChunk) int {
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

// parseRequestID converts the string ctx value into a typed RequestID,
// falling back to id.Nil when absent or malformed.
func parseRequestID(s string) id.RequestID {
	if s == "" {
		return id.Nil
	}
	parsed, err := id.ParseRequestID(s)
	if err != nil {
		// Some pipelines may set an opaque request id; keep the typed value
		// nil rather than panicking. Hooks still receive the raw string via
		// pipeline.RequestID(ctx) if they need it.
		return id.Nil
	}
	return parsed
}

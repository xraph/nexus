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
// Position: priority 60, inside the usage stage (15), so the usage stage's
// stream wrapper closes it first and reads the merged final response it
// publishes.
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
func (m *StreamLifecycleMiddleware) Priority() int { return 60 }

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
// the merged final response. Next must not be called concurrently with
// itself, same as the underlying Stream contract. Close may arrive from
// another goroutine while a Next is in flight (a shutdown cancels the stream
// under its reader), and the quota watchdog runs on its own goroutine, so mu
// guards every field they share. mu is never held across inner.Next or a
// hook.
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

	once sync.Once

	// closeUpstream closes the inner stream at most once, whoever asks first:
	// Close, or the watchdog. closeErr is what that one close returned.
	closeUpstreamOnce sync.Once
	closeErr          error

	// mu guards the fields below.
	mu           sync.Mutex
	closed       bool // Close has run; no watchdog may start
	startedFired bool
	chunkCount   int

	// accumulator merges deltas as they pass through. Built lazily so the
	// hot path stays cheap when no completion-style hook is registered.
	acc *provider.Accumulator

	// Quota enforcement state. quotaErr is non-nil when a quota is active;
	// when the watchdog or per-chunk check trips, it sends the violation
	// error and Next returns it on the next invocation. quota and quotaErr
	// are set before the stream is returned and never change.
	quota          StreamQuota
	quotaErr       chan error
	quotaTokenSeen int
	watchdogStop   chan struct{}
	cutUsage       *provider.Usage // the usage chunk that tripped the token cap
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

	// The duration watchdog sends its error and then closes the inner stream.
	// A consumer already parked in inner.Next gets the close as an io.EOF or a
	// provider error, which is not what ended the stream. The watchdog sent
	// before it closed, so the quota error is already waiting.
	if err != nil && s.quotaErr != nil {
		select {
		case qe := <-s.quotaErr:
			s.finishOnce(ctx, qe)
			return nil, qe
		default:
		}
	}

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

	first, emit, cut := s.record(chunk)
	if first {
		s.registry.EmitStreamStarted(s.ctx, s.requestID, s.model, s.providerName)
	}
	if emit {
		s.registry.EmitChunkReceived(s.ctx, s.requestID, chunk.Kind, estimateChunkSize(chunk))
	}
	if cut != nil {
		s.finishOnce(ctx, cut)
		return nil, cut
	}

	return chunk, nil
}

// record folds one chunk into the stream's state under mu. It reports whether
// this was the first chunk, whether the chunk-received hook should fire, and
// the quota error when this chunk tripped the token cap. Hooks run in the
// caller, outside the lock.
func (s *lifecycleStream) record(chunk *provider.StreamChunk) (first, emit bool, cut error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.startedFired {
		s.startedFired = true
		first = true
		s.startWatchdogLocked()
	}

	s.chunkCount++
	emit = s.shouldEmitChunkLocked()

	if s.acc == nil {
		s.acc = provider.NewAccumulator()
	}
	s.acc.Add(chunk)

	if s.quota.MaxTokens > 0 && chunk.Usage != nil {
		s.quotaTokenSeen = chunk.Usage.CompletionTokens
		if s.quotaTokenSeen > s.quota.MaxTokens {
			// The chunk is withheld, but the provider reported these tokens:
			// keep them so Usage still prices what was used.
			u := *chunk.Usage
			s.cutUsage = &u
			return first, emit, errQuotaExceeded("output_tokens")
		}
	}
	return first, emit, nil
}

// startWatchdogLocked kicks off a goroutine that fires when MaxDuration
// elapses, closing the upstream stream so the next Next() call surfaces an
// error. The caller holds mu. A stream already closed gets no watchdog: its
// first chunk can arrive from a Next that was in flight when Close ran.
func (s *lifecycleStream) startWatchdogLocked() {
	if s.closed || s.quotaErr == nil || s.quota.MaxDuration <= 0 {
		return
	}
	stop := make(chan struct{})
	s.watchdogStop = stop
	go func() {
		t := time.NewTimer(s.quota.MaxDuration)
		defer t.Stop()
		select {
		case <-t.C:
			select {
			case s.quotaErr <- errQuotaExceeded("duration"):
			default:
			}
			s.closeUpstream()
		case <-stop:
		}
	}()
}

// closeUpstream closes the inner stream the first time anyone asks; later
// callers wait for that close and change nothing. The error it returned is
// closeErr.
func (s *lifecycleStream) closeUpstream() {
	s.closeUpstreamOnce.Do(func() { s.closeErr = s.inner.Close() })
}

// QuotaError is the typed sentinel emitted when a stream is canceled by
// the lifecycle middleware's quota watchdog. Use IsQuotaExceeded to detect.
//
// It must only come from a stream's Next, never from Process: the usage
// stage classifies a Refusal returned by Process as refused at $0, and a cut
// stream was served in part and is priced.
type QuotaError struct{ What string }

var _ pipeline.Refusal = &QuotaError{}

func (e *QuotaError) Error() string { return "nexus: stream quota exceeded: " + e.What }

func errQuotaExceeded(what string) error { return &QuotaError{What: what} }

// RefusalCode and StatusCode report a stream cut by its tenant's limit as
// quota_exceeded, 429. The stream was served in part, so the usage stage
// still prices the tokens it saw.
func (e *QuotaError) RefusalCode() string { return pipeline.CodeQuotaExceeded }
func (e *QuotaError) StatusCode() int     { return 429 }

// TenantStreamQuota is the default QuotaResolver: the stream limits of the
// tenant the access stage loaded, or none.
func TenantStreamQuota(ctx context.Context) StreamQuota {
	t, ok := TenantFromContext(ctx)
	if !ok {
		return StreamQuota{}
	}
	return StreamQuota{MaxDuration: t.Quota.MaxStreamDuration, MaxTokens: t.Quota.MaxStreamTokens}
}

// IsQuotaExceeded reports whether err is a stream-quota violation.
func IsQuotaExceeded(err error) bool {
	var qe *QuotaError
	return errors.As(err, &qe)
}

// shouldEmitChunkLocked reports whether this chunk's hook should fire. The
// caller holds mu, and chunkCount already counts the chunk.
func (s *lifecycleStream) shouldEmitChunkLocked() bool {
	if !s.registry.HasChunkReceived() {
		return false
	}
	if s.emitEvery <= 1 {
		return true
	}
	return s.chunkCount == 1 || (s.chunkCount%s.emitEvery == 0)
}

func (s *lifecycleStream) Close() error {
	s.mu.Lock()
	s.closed = true
	if s.watchdogStop != nil {
		close(s.watchdogStop)
		s.watchdogStop = nil
	}
	s.mu.Unlock()

	s.finishOnce(s.ctx, nil)
	s.closeUpstream()
	return s.closeErr
}

func (s *lifecycleStream) Usage() *provider.Usage {
	if u := s.inner.Usage(); u != nil {
		return u
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cutUsage
}

func (s *lifecycleStream) finishOnce(ctx context.Context, streamErr error) {
	s.once.Do(func() {
		elapsed := time.Since(s.startedAt)
		if streamErr != nil {
			s.registry.EmitStreamFailed(ctx, s.requestID, s.model, streamErr)
			return
		}
		final := s.buildFinal()
		if s.req != nil {
			if s.req.State == nil {
				s.req.State = make(map[string]any)
			}
			s.req.State[StateKeyStreamFinalResponse] = final
		}
		s.registry.EmitStreamCompleted(ctx, s.requestID, s.model, s.providerName, elapsed, final)
	})
}

func (s *lifecycleStream) buildFinal() *provider.CompletionResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
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

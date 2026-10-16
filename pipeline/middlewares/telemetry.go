package middlewares

import (
	"context"
	"errors"
	"io"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	gometrics "github.com/xraph/go-utils/metrics"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/internal/telemetry"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

// Request outcomes, as telemetry reports them. They match the usage
// record's outcomes.
const (
	outcomeOK      = "ok"
	outcomeCached  = "cached"
	outcomeBlocked = "blocked"
	outcomeRefused = "refused"
	outcomeError   = "error"
)

// knownCodes are the refusal codes telemetry reports by name. A custom
// stage's own code is reported as "other".
var knownCodes = map[string]bool{
	pipeline.CodeUnauthenticated: true,
	pipeline.CodeForbidden:       true,
	pipeline.CodeInvalidRequest:  true,
	pipeline.CodeRateLimited:     true,
	pipeline.CodeQuotaExceeded:   true,
	pipeline.CodeBudgetExceeded:  true,
	pipeline.CodeUnavailable:     true,
	pipeline.CodeContentBlocked:  true,
}

// TelemetryMiddleware traces each request through the pipeline and counts
// how it ended. It sits at priority 10, outside everything but the request
// id, so its span is the parent of every stage's work and of the provider
// call, and it sees the final outcome after retries.
//
// The span is "serve completion", "serve stream" or "serve embedding". A
// stream's span ends when the stream is closed. It carries the request type,
// request id, tenant id, provider, cache hit, outcome and refusal code; never
// a prompt, a completion or a key.
type TelemetryMiddleware struct {
	requests *telemetry.Counter
	blocks   *telemetry.Counter
}

// NewTelemetry returns the telemetry stage. Spans need nothing more: they go
// to the global tracer provider. Call WithMetrics to count requests too.
func NewTelemetry() *TelemetryMiddleware { return &TelemetryMiddleware{} }

// WithMetrics counts requests (nexus_requests_total) and guardrail blocks
// (nexus_guardrail_blocks_total) on mm. A nil mm counts nothing.
func (m *TelemetryMiddleware) WithMetrics(mm gometrics.Metrics) *TelemetryMiddleware {
	m.requests = telemetry.NewCounter(mm, telemetry.MetricRequests,
		"Requests through the Nexus pipeline, by type, outcome and refusal code.",
		telemetry.LabelType, telemetry.LabelOutcome, telemetry.LabelCode)
	m.blocks = telemetry.NewCounter(mm, telemetry.MetricGuardrailBlocks,
		"Requests a guardrail blocked, by phase and guard.",
		telemetry.LabelPhase, telemetry.LabelGuard)
	return m
}

func (m *TelemetryMiddleware) Name() string  { return "telemetry" }
func (m *TelemetryMiddleware) Priority() int { return 10 }

func (m *TelemetryMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	attrs := []attribute.KeyValue{telemetry.RequestType.String(string(req.Type))}
	if rid := pipeline.RequestID(ctx); rid != "" {
		attrs = append(attrs, telemetry.RequestID.String(rid))
	}
	ctx, span := telemetry.Tracer().Start(ctx, "serve "+string(req.Type), //nolint:spancheck // ended by finish
		trace.WithSpanKind(trace.SpanKindInternal), trace.WithAttributes(attrs...))
	defer func() {
		if r := recover(); r != nil {
			span.SetStatus(codes.Error, "panic")
			span.End()
			panic(r)
		}
	}()

	resp, err := next(ctx)
	if err == nil && resp != nil && resp.Stream != nil {
		resp.Stream = &telemetryStream{inner: resp.Stream, mw: m, ctx: ctx, span: span, req: req}
		return resp, nil
	}
	o := classifyRequest(req, err)
	m.finish(ctx, span, req, o, err)
	return resp, err
}

// requestOutcome is how a request ended.
type requestOutcome struct {
	outcome string
	code    string
	blocked *guard.BlockedError
}

// classifyRequest classifies a request that did not open a stream: a guard
// block, a refusal, an error, a cache hit or a success, the way the usage
// stage records it.
func classifyRequest(req *pipeline.Request, err error) requestOutcome {
	var blocked *guard.BlockedError
	var refused pipeline.Refusal
	switch {
	case errors.As(err, &blocked):
		return requestOutcome{outcome: outcomeBlocked, code: pipeline.CodeContentBlocked, blocked: blocked}
	case errors.As(err, &refused):
		return requestOutcome{outcome: outcomeRefused, code: knownCode(refused.RefusalCode())}
	case err != nil:
		return requestOutcome{outcome: outcomeError}
	case cacheHit(req):
		return requestOutcome{outcome: outcomeCached}
	}
	return requestOutcome{outcome: outcomeOK}
}

// classifyStream classifies a stream from the first error its Next returned.
// A stream cut by its tenant's limit stays an error under quota_exceeded, as
// in the usage record: it was served in part.
func classifyStream(req *pipeline.Request, err error) requestOutcome {
	var blocked *guard.BlockedError
	var refused pipeline.Refusal
	switch {
	case errors.As(err, &blocked):
		return requestOutcome{outcome: outcomeBlocked, code: pipeline.CodeContentBlocked, blocked: blocked}
	case errors.As(err, &refused):
		return requestOutcome{outcome: outcomeError, code: knownCode(refused.RefusalCode())}
	case err != nil:
		return requestOutcome{outcome: outcomeError}
	case cacheHit(req):
		return requestOutcome{outcome: outcomeCached}
	}
	return requestOutcome{outcome: outcomeOK}
}

// cacheHit reports whether the cache stage served the request.
func cacheHit(req *pipeline.Request) bool {
	v, ok := req.State[pipeline.StateCacheHit].(bool)
	return ok && v
}

func knownCode(code string) string {
	if knownCodes[code] {
		return code
	}
	return "other"
}

// finish sets the outcome on the span, ends it and counts the request.
func (m *TelemetryMiddleware) finish(ctx context.Context, span trace.Span, req *pipeline.Request, o requestOutcome, err error) {
	if span.IsRecording() {
		attrs := []attribute.KeyValue{
			telemetry.Outcome.String(o.outcome),
			telemetry.CacheHit.Bool(cacheHit(req)),
		}
		if o.code != "" {
			attrs = append(attrs, telemetry.RefusalCode.String(o.code))
		}
		if o.blocked != nil {
			attrs = append(attrs, telemetry.GuardrailName.String(o.blocked.Guard), telemetry.GuardrailPhase.String(string(o.blocked.Phase)))
		}
		if p := stateString(req, pipeline.StateProviderName); p != "" {
			attrs = append(attrs, telemetry.ProviderName.String(p))
		}
		tenant, _ := requestIdentity(req)
		if t := pipeline.TenantID(ctx); t != "" {
			tenant = t
		}
		if tenant != "" {
			attrs = append(attrs, telemetry.TenantID.String(tenant))
		}
		span.SetAttributes(attrs...)
	}
	if o.outcome == outcomeError {
		telemetry.Fail(span, err)
	}
	span.End()

	m.requests.Inc(string(req.Type), o.outcome, o.code)
	if o.blocked != nil {
		m.blocks.Inc(string(o.blocked.Phase), o.blocked.Guard)
	}
}

// telemetryStream ends the request's span when the stream is closed, with
// the outcome the stream's errors and the request's state give.
type telemetryStream struct {
	inner provider.Stream
	mw    *TelemetryMiddleware
	ctx   context.Context
	span  trace.Span
	req   *pipeline.Request

	mu     sync.Mutex
	err    error // the first error Next returned that was not io.EOF
	closed bool
}

func (s *telemetryStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	chunk, err := s.inner.Next(ctx)
	if err != nil && !errors.Is(err, io.EOF) {
		s.mu.Lock()
		if s.err == nil {
			s.err = err
		}
		s.mu.Unlock()
	}
	return chunk, err
}

func (s *telemetryStream) Close() error {
	closeErr := s.inner.Close()
	s.mu.Lock()
	already, streamErr := s.closed, s.err
	s.closed = true
	s.mu.Unlock()
	if !already {
		s.mw.finish(s.ctx, s.span, s.req, classifyStream(s.req, streamErr), streamErr)
	}
	return closeErr
}

func (s *telemetryStream) Usage() *provider.Usage { return s.inner.Usage() }

package middlewares

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	gometrics "github.com/xraph/go-utils/metrics"

	"github.com/xraph/nexus/internal/telemetry"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

// ModelCatalog reports whether a provider lists a model. Telemetry names a
// model only when the provider that serves it lists it and reports any other
// name as "other", so a client's arbitrary model string never becomes a metric
// series or a span attribute. *model.PriceBook implements it.
type ModelCatalog interface {
	Lists(ctx context.Context, providerName, modelID string) bool
}

// genAIMetrics are the provider call's instruments. A nil *genAIMetrics, or
// any nil field, records nothing.
type genAIMetrics struct {
	duration   *telemetry.Histogram
	tokens     *telemetry.Histogram
	firstChunk *telemetry.Histogram
}

func newGenAIMetrics(m gometrics.Metrics) *genAIMetrics {
	if m == nil {
		return nil
	}
	return &genAIMetrics{
		duration: telemetry.NewHistogram(m, telemetry.MetricOperationDuration,
			"Duration of GenAI provider calls, a stream until its last chunk.", "s", telemetry.OperationDurationBuckets,
			telemetry.LabelOperation, telemetry.LabelProvider, telemetry.LabelModel, telemetry.LabelErrorType),
		tokens: telemetry.NewHistogram(m, telemetry.MetricTokenUsage,
			"Tokens a GenAI provider call used, by token type.", "{token}", telemetry.TokenUsageBuckets,
			telemetry.LabelOperation, telemetry.LabelProvider, telemetry.LabelModel, telemetry.LabelTokenType),
		firstChunk: telemetry.NewHistogram(m, telemetry.MetricFirstChunk,
			"Time from a streaming provider call to its first chunk.", "s", telemetry.FirstChunkBuckets,
			telemetry.LabelProvider, telemetry.LabelModel),
	}
}

// genAICall is one call to a provider: a client span named after the GenAI
// operation and model, and the duration and token metrics recorded when it
// ends. One attempt is one call, so a retried request has one per attempt.
type genAICall struct {
	metrics   *genAIMetrics
	catalog   ModelCatalog
	span      trace.Span
	start     time.Time
	op        string
	provider  string // Nexus registry name
	genAIName string // gen_ai.provider.name
	model     string // the requested model when listed, else "other"

	once  sync.Once
	ended atomic.Bool
}

// startGenAICall starts the span for a call to p. The span is a client span
// named "{operation} {model}", or just the operation when p does not list the
// model.
func (m *ProviderCallMiddleware) startGenAICall(ctx context.Context, op string, p provider.Provider, reqType pipeline.RequestType, requested string, attrs ...attribute.KeyValue) (context.Context, *genAICall) {
	c := &genAICall{
		metrics:   m.metrics,
		catalog:   m.catalog,
		start:     time.Now(),
		op:        op,
		provider:  p.Name(),
		genAIName: telemetry.GenAIProvider(p.Name()),
		model:     telemetry.OtherModel,
	}
	name := op
	attrs = append(attrs,
		telemetry.GenAIOperationName.String(op),
		telemetry.GenAIProviderName.String(c.genAIName),
		telemetry.ProviderName.String(c.provider),
		telemetry.RequestType.String(string(reqType)),
	)
	if c.listed(ctx, requested) {
		c.model = requested
		name = op + " " + requested
		attrs = append(attrs, telemetry.GenAIRequestModel.String(requested))
	}
	ctx, c.span = telemetry.Tracer().Start(ctx, name, //nolint:spancheck // ended by genAICall.end
		trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return ctx, c
}

func (c *genAICall) listed(ctx context.Context, model string) bool {
	return c.catalog != nil && model != "" && c.catalog.Lists(ctx, c.provider, model)
}

// genAIResult is what a call reported back.
type genAIResult struct {
	id       string
	model    string
	finish   []string
	input    int
	output   int
	hasUsage bool
}

// end ends the span and records the metrics, once. A nil err is a success.
func (c *genAICall) end(ctx context.Context, res genAIResult, err error) {
	c.once.Do(func() {
		c.ended.Store(true)
		elapsed := time.Since(c.start).Seconds()
		errType := ""
		if c.span.IsRecording() {
			var attrs []attribute.KeyValue
			if res.id != "" {
				attrs = append(attrs, telemetry.GenAIResponseID.String(res.id))
			}
			if c.listed(ctx, res.model) {
				attrs = append(attrs, telemetry.GenAIResponseModel.String(res.model))
			}
			if len(res.finish) > 0 {
				attrs = append(attrs, telemetry.GenAIResponseFinishReasons.StringSlice(res.finish))
			}
			if res.hasUsage {
				attrs = append(attrs, telemetry.GenAIUsageInputTokens.Int(res.input))
				if c.op == telemetry.OperationChat {
					attrs = append(attrs, telemetry.GenAIUsageOutputTokens.Int(res.output))
				}
			}
			c.span.SetAttributes(attrs...)
		}
		if err != nil {
			errType = telemetry.Fail(c.span, err)
		}
		c.span.End()

		if c.metrics == nil {
			return
		}
		c.metrics.duration.Observe(elapsed, c.op, c.genAIName, c.model, errType)
		if res.hasUsage {
			if res.input > 0 {
				c.metrics.tokens.Observe(float64(res.input), c.op, c.genAIName, c.model, "input")
			}
			if res.output > 0 && c.op == telemetry.OperationChat {
				c.metrics.tokens.Observe(float64(res.output), c.op, c.genAIName, c.model, "output")
			}
		}
	})
}

func completionResult(resp *provider.CompletionResponse) genAIResult {
	if resp == nil {
		return genAIResult{}
	}
	res := genAIResult{id: resp.ID, model: resp.Model}
	for _, ch := range resp.Choices {
		if ch.FinishReason != "" && !slices.Contains(res.finish, ch.FinishReason) {
			res.finish = append(res.finish, ch.FinishReason)
		}
	}
	res.setUsage(resp.Usage)
	return res
}

func embeddingResult(resp *provider.EmbeddingResponse) genAIResult {
	if resp == nil {
		return genAIResult{}
	}
	res := genAIResult{model: resp.Model}
	u := resp.Usage
	if u.PromptTokens == 0 {
		u.PromptTokens = u.TotalTokens
	}
	res.setUsage(u)
	return res
}

func (r *genAIResult) setUsage(u provider.Usage) {
	if u.PromptTokens == 0 && u.CompletionTokens == 0 {
		return
	}
	r.input, r.output, r.hasUsage = u.PromptTokens, u.CompletionTokens, true
}

// requestAttributes are the request parameters the GenAI conventions record:
// numbers only, never the messages.
func requestAttributes(req *provider.CompletionRequest) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if req.MaxTokens > 0 {
		attrs = append(attrs, telemetry.GenAIRequestMaxTokens.Int(req.MaxTokens))
	}
	if req.Temperature != nil {
		attrs = append(attrs, telemetry.GenAIRequestTemperature.Float64(*req.Temperature))
	}
	if req.TopP != nil {
		attrs = append(attrs, telemetry.GenAIRequestTopP.Float64(*req.TopP))
	}
	return attrs
}

// genAIStream ends its call when the stream does: at io.EOF, at the first
// other error, or at Close, whichever comes first. It records the time to the
// first chunk that is not a heartbeat.
type genAIStream struct {
	inner provider.Stream
	call  *genAICall
	ctx   context.Context

	mu      sync.Mutex
	started bool
	res     genAIResult
}

func (s *genAIStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	chunk, err := s.inner.Next(ctx)
	if chunk != nil {
		s.observe(chunk)
	}
	switch {
	case errors.Is(err, io.EOF):
		s.finish(nil)
	case err != nil:
		s.finish(err)
	}
	return chunk, err
}

func (s *genAIStream) observe(chunk *provider.StreamChunk) {
	s.mu.Lock()
	first := !s.started && chunk.Kind != provider.EventHeartbeat
	if first {
		s.started = true
	}
	if chunk.ID != "" {
		s.res.id = chunk.ID
	}
	if chunk.Model != "" {
		s.res.model = chunk.Model
	}
	if chunk.FinishReason != "" && !slices.Contains(s.res.finish, chunk.FinishReason) {
		s.res.finish = append(s.res.finish, chunk.FinishReason)
	}
	if chunk.Usage != nil {
		s.res.setUsage(*chunk.Usage)
	}
	s.mu.Unlock()

	if first {
		c := s.call
		c.span.AddEvent("first chunk")
		if c.metrics != nil {
			c.metrics.firstChunk.Observe(time.Since(c.start).Seconds(), c.genAIName, c.model)
		}
	}
}

// finish ends the call with what the stream reported. A stream that sent no
// usage chunk falls back to the stream's own Usage.
func (s *genAIStream) finish(err error) {
	if s.call.ended.Load() {
		return
	}
	s.mu.Lock()
	res := s.res
	res.finish = slices.Clone(s.res.finish)
	s.mu.Unlock()
	if !res.hasUsage {
		if u := s.inner.Usage(); u != nil {
			res.setUsage(*u)
		}
	}
	s.call.end(s.ctx, res, err)
}

func (s *genAIStream) Close() error {
	err := s.inner.Close()
	s.finish(nil)
	return err
}

func (s *genAIStream) Usage() *provider.Usage { return s.inner.Usage() }

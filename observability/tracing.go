// Package observability provides tracing and metrics middleware for Nexus.
package observability

import (
	"context"
	"time"

	"github.com/xraph/nexus/pipeline"
)

// Tracer creates spans for request tracing.
type Tracer interface {
	// StartSpan begins a new span. Returns a context with the span and a finish func.
	StartSpan(ctx context.Context, name string, attrs ...SpanAttribute) (context.Context, SpanFinisher)
}

// SpanFinisher completes a span.
type SpanFinisher interface {
	// Finish completes the span. If err is non-nil, the span is marked as errored.
	Finish(err error)

	// SetAttribute adds an attribute to the span.
	SetAttribute(key string, value any)
}

// SpanAttribute is a key-value pair for span metadata.
type SpanAttribute struct {
	Key   string
	Value any
}

// TracingMiddleware adds distributed tracing to the pipeline.
type TracingMiddleware struct {
	tracer Tracer
}

// NewTracingMiddleware creates a tracing middleware with the given tracer.
func NewTracingMiddleware(tracer Tracer) *TracingMiddleware {
	return &TracingMiddleware{tracer: tracer}
}

func (m *TracingMiddleware) Name() string  { return "tracing" }
func (m *TracingMiddleware) Priority() int { return 10 } // Very early — wraps everything

func (m *TracingMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.tracer == nil {
		return next(ctx)
	}

	model := ""
	if req.Completion != nil {
		model = req.Completion.Model
	}

	ctx, span := m.tracer.StartSpan(ctx, "nexus.request",
		SpanAttribute{Key: "nexus.request.type", Value: string(req.Type)},
		SpanAttribute{Key: "nexus.request.model", Value: model},
		SpanAttribute{Key: "nexus.request.id", Value: pipeline.RequestID(ctx)},
		SpanAttribute{Key: "nexus.tenant.id", Value: pipeline.TenantID(ctx)},
	)

	start := time.Now()
	resp, err := next(ctx)

	span.SetAttribute("nexus.request.duration_ms", time.Since(start).Milliseconds())
	if providerName, ok := req.State["provider_name"].(string); ok {
		span.SetAttribute("nexus.provider.name", providerName)
	}
	if resp != nil && resp.Completion != nil {
		span.SetAttribute("nexus.tokens.input", resp.Completion.Usage.PromptTokens)
		span.SetAttribute("nexus.tokens.output", resp.Completion.Usage.CompletionTokens)
	}

	span.Finish(err)
	return resp, err
}

// NoopTracer is a tracer that does nothing.
type NoopTracer struct{}

func (NoopTracer) StartSpan(ctx context.Context, _ string, _ ...SpanAttribute) (context.Context, SpanFinisher) {
	return ctx, &noopSpan{}
}

type noopSpan struct{}

func (noopSpan) Finish(error)             {}
func (noopSpan) SetAttribute(string, any) {}

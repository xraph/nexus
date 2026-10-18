// Package telemetry holds the OpenTelemetry plumbing Nexus's stages share:
// the tracer, the span attribute keys, how an error is named, and the metric
// families recorded on a go-utils metrics sink.
//
// Spans go to the global tracer provider, which forge.New installs. Without
// one they cost a no-op tracer call. Metrics are recorded only on a sink the
// caller hands over (nexus.WithMetrics), never on otel.Meter: a Forge app
// installs no global MeterProvider, so nothing recorded there is exported.
package telemetry

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentationName is the instrumentation scope on every span Nexus starts.
const InstrumentationName = "github.com/xraph/nexus"

// Tracer returns the tracer Nexus starts its spans with. It reads the global
// tracer provider on every call, so whichever provider is installed when the
// span starts is the one that records it.
func Tracer() trace.Tracer { return otel.Tracer(InstrumentationName) }

// Span attribute keys. The gen_ai ones follow the OpenTelemetry GenAI
// semantic conventions; the nexus ones are this module's own.
const (
	GenAIOperationName         = attribute.Key("gen_ai.operation.name")
	GenAIProviderName          = attribute.Key("gen_ai.provider.name")
	GenAIRequestModel          = attribute.Key("gen_ai.request.model")
	GenAIRequestMaxTokens      = attribute.Key("gen_ai.request.max_tokens")
	GenAIRequestTemperature    = attribute.Key("gen_ai.request.temperature")
	GenAIRequestTopP           = attribute.Key("gen_ai.request.top_p")
	GenAIResponseModel         = attribute.Key("gen_ai.response.model")
	GenAIResponseID            = attribute.Key("gen_ai.response.id")
	GenAIResponseFinishReasons = attribute.Key("gen_ai.response.finish_reasons")
	GenAIUsageInputTokens      = attribute.Key("gen_ai.usage.input_tokens")
	GenAIUsageOutputTokens     = attribute.Key("gen_ai.usage.output_tokens")
	ErrorTypeKey               = attribute.Key("error.type")

	ProviderName    = attribute.Key("nexus.provider.name")
	RequestType     = attribute.Key("nexus.request.type")
	RequestID       = attribute.Key("nexus.request.id")
	TenantID        = attribute.Key("nexus.tenant.id")
	Outcome         = attribute.Key("nexus.outcome")
	RefusalCode     = attribute.Key("nexus.refusal.code")
	CacheHit        = attribute.Key("nexus.cache.hit")
	CacheKind       = attribute.Key("nexus.cache.kind")
	GuardrailPhase  = attribute.Key("nexus.guardrail.phase")
	GuardrailName   = attribute.Key("nexus.guardrail.name")
	GuardrailBlock  = attribute.Key("nexus.guardrail.blocked")
	GuardrailEdited = attribute.Key("nexus.guardrail.modified")
	UsageOutcome    = attribute.Key("nexus.usage.outcome")
	UsagePricing    = attribute.Key("nexus.usage.pricing_status")
	BatchJobID      = attribute.Key("nexus.batch.job.id")
	BatchItems      = attribute.Key("nexus.batch.items")
	BatchFailed     = attribute.Key("nexus.batch.failed")
	BatchStatus     = attribute.Key("nexus.batch.status")
)

// GenAI operation names.
const (
	OperationChat       = "chat"
	OperationEmbeddings = "embeddings"
)

// OtherModel stands in for a model name its provider does not list.
const OtherModel = "other"

// Detach returns a fresh context that carries nothing but ctx's span context.
// Work that has to outlive ctx (storing a usage record, charging tokens,
// writing a stream to the cache) starts from it: it is not canceled with the
// request and inherits none of its values, yet its spans still land in the
// request's trace.
func Detach(ctx context.Context) context.Context {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return context.Background()
	}
	return trace.ContextWithSpanContext(context.Background(), sc)
}

// ErrorType names err for error.type and the exception event: the context
// error it is, or the Go type of the innermost error in its chain. It never
// returns the error's text, which can quote a prompt, a key or a provider's
// reply. A nil err is "".
func ErrorType(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "context.DeadlineExceeded"
	case errors.Is(err, context.Canceled):
		return "context.Canceled"
	}
	for next := errors.Unwrap(err); next != nil; next = errors.Unwrap(err) {
		err = next
	}
	return fmt.Sprintf("%T", err)
}

// Fail marks span as failed: error.type, an exception event that carries only
// exception.type, and an error status whose description is that type. It
// returns the type. The error's text never reaches the span.
func Fail(span trace.Span, err error) string {
	t := ErrorType(err)
	if t == "" || !span.IsRecording() {
		return t
	}
	span.SetAttributes(ErrorTypeKey.String(t))
	span.AddEvent("exception", trace.WithAttributes(attribute.String("exception.type", t)))
	span.SetStatus(codes.Error, t)
	return t
}

// genAIProviders maps the Nexus provider names whose gen_ai.provider.name
// differs to the value the GenAI conventions list. Every other provider is
// reported under its Nexus name.
var genAIProviders = map[string]string{
	"azureopenai": "azure.ai.openai",
	"bedrock":     "aws.bedrock",
	"gemini":      "gcp.gemini",
	"mistral":     "mistral_ai",
	"vertex":      "gcp.vertex_ai",
	"xai":         "x_ai",
}

// GenAIProvider returns gen_ai.provider.name for a Nexus provider name.
func GenAIProvider(name string) string {
	if v, ok := genAIProviders[name]; ok {
		return v
	}
	return name
}

package middlewares_test

import (
	"context"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/internal/telemetry"
	"github.com/xraph/nexus/internal/telemetry/telemetrytest"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/ratelimit"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/testutil"
)

// leak is a value that must never reach a span: it stands for an email in a
// prompt, a tenant's metadata, a guard's reason or a provider's error text.
const leak = "alice@example.com"

var gpt4oModels = []provider.Model{{ID: "gpt-4o"}, {ID: "text-embedding-3-small"}}

// chatFake answers completions and streams like a provider would.
type chatFake struct {
	listedFake
	err     error
	failN   int // fail this many calls first
	calls   int
	mu      sync.Mutex
	chunks  []*provider.StreamChunk
	gotSpan trace.SpanContext
	baggage int // members of the baggage the provider was called with
}

func (f *chatFake) Complete(ctx context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.gotSpan = trace.SpanContextFromContext(ctx)
	f.baggage = baggage.FromContext(ctx).Len()
	f.mu.Unlock()
	if f.err != nil && n <= f.failN {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *chatFake) CompleteStream(ctx context.Context, _ *provider.CompletionRequest) (provider.Stream, error) {
	f.gotSpan = trace.SpanContextFromContext(ctx)
	if f.err != nil {
		return nil, f.err
	}
	return testutil.NewFakeStream(f.chunks, nil), nil
}

func newChatFake() *chatFake {
	return &chatFake{listedFake: listedFake{
		name:   "openai",
		models: gpt4oModels,
		resp: &provider.CompletionResponse{
			ID: "chatcmpl-1", Provider: "openai", Model: "gpt-4o",
			Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: leak}, FinishReason: "stop"}},
			Usage:   provider.Usage{PromptTokens: 12, CompletionTokens: 34, TotalTokens: 46},
		},
		emb: &provider.EmbeddingResponse{Model: "text-embedding-3-small", Usage: provider.Usage{PromptTokens: 7, TotalTokens: 7}},
	}}
}

// build assembles telemetry, the given stages and the provider call, all
// recording on sink.
func build(t *testing.T, sink *telemetrytest.Sink, p provider.Provider, stages ...pipeline.Middleware) pipeline.Service {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(p)
	b := pipeline.NewBuilder().Use(middlewares.NewTelemetry().WithMetrics(sink))
	b.Use(stages...)
	b.Use(middlewares.NewProviderCall(nil, reg).WithMetrics(sink))
	svc, err := b.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return svc
}

// parent starts the span that stands in for forge's server span.
func parent(t *testing.T) (context.Context, trace.Span) {
	t.Helper()
	ctx := pipeline.WithRequestID(context.Background(), id.NewRequestID().String())
	return telemetry.Tracer().Start(ctx, "POST /v1/chat/completions", trace.WithSpanKind(trace.SpanKindServer))
}

func chatRequest(model string) *provider.CompletionRequest {
	temp := 0.2
	return &provider.CompletionRequest{
		Model:       model,
		Messages:    []provider.Message{{Role: "user", Content: "my email is " + leak}},
		MaxTokens:   256,
		Temperature: &temp,
		TenantID:    "tenant_01",
		Metadata:    map[string]string{"user": leak},
	}
}

func childOf(t *testing.T, child, parent sdktrace.ReadOnlySpan) {
	t.Helper()
	if child.Parent().SpanID() != parent.SpanContext().SpanID() || child.SpanContext().TraceID() != parent.SpanContext().TraceID() {
		t.Fatalf("%q is not a child of %q", child.Name(), parent.Name())
	}
}

func wantAttrs(t *testing.T, span sdktrace.ReadOnlySpan, want map[string]string) {
	t.Helper()
	got := telemetrytest.Attrs(span)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s = %q, want %q", span.Name(), k, got[k], v)
		}
	}
}

func TestCompletionTracesThePipelineAndTheProviderCall(t *testing.T) {
	rec := telemetrytest.Spans(t)
	sink := telemetrytest.NewSink()
	p := newChatFake()
	svc := build(t, sink, p)

	ctx, server := parent(t)
	if _, err := svc.Execute(ctx, chatRequest("gpt-4o")); err != nil {
		t.Fatalf("execute: %v", err)
	}
	server.End()

	serve := telemetrytest.One(t, rec, "serve completion")
	chat := telemetrytest.One(t, rec, "chat gpt-4o")
	childOf(t, serve, telemetrytest.One(t, rec, "POST /v1/chat/completions"))
	childOf(t, chat, serve)
	if p.gotSpan.SpanID() != chat.SpanContext().SpanID() {
		t.Fatal("the provider was not called under the chat span, so its HTTP span would not nest")
	}
	if serve.SpanKind() != trace.SpanKindInternal || chat.SpanKind() != trace.SpanKindClient {
		t.Fatalf("kinds: serve %v, chat %v", serve.SpanKind(), chat.SpanKind())
	}
	wantAttrs(t, serve, map[string]string{
		"nexus.request.type": "completion", "nexus.outcome": "ok", "nexus.provider.name": "openai",
		"nexus.cache.hit": "false", "nexus.tenant.id": "tenant_01",
	})
	if telemetrytest.Attrs(serve)["nexus.request.id"] == "" {
		t.Error("serve span has no request id")
	}
	wantAttrs(t, chat, map[string]string{
		"gen_ai.operation.name": "chat", "gen_ai.provider.name": "openai", "nexus.provider.name": "openai",
		"gen_ai.request.model": "gpt-4o", "gen_ai.response.model": "gpt-4o", "gen_ai.response.id": "chatcmpl-1",
		"gen_ai.response.finish_reasons": `["stop"]`, "gen_ai.usage.input_tokens": "12", "gen_ai.usage.output_tokens": "34",
		"gen_ai.request.max_tokens": "256", "gen_ai.request.temperature": "0.2", "nexus.request.type": "completion",
	})
	if serve.Status().Code == codes.Error || chat.Status().Code == codes.Error {
		t.Fatal("a served request was marked failed")
	}
	telemetrytest.AssertAbsent(t, rec, leak)

	labels := map[string]string{"gen_ai.operation.name": "chat", "gen_ai.provider.name": "openai", "gen_ai.request.model": "gpt-4o", "error.type": ""}
	if s, ok := sink.Find(telemetry.MetricOperationDuration, labels); !ok || s.Count != 1 || s.Unit != "s" {
		t.Fatalf("duration series = %+v (%v); all: %+v", s, ok, sink.Series(telemetry.MetricOperationDuration))
	}
	for typ, n := range map[string]float64{"input": 12, "output": 34} {
		s, ok := sink.Find(telemetry.MetricTokenUsage, map[string]string{"gen_ai.token.type": typ, "gen_ai.request.model": "gpt-4o"})
		if !ok || s.Count != 1 || s.Sum != n || s.Unit != "{token}" {
			t.Errorf("%s tokens = %+v (%v)", typ, s, ok)
		}
	}
	if s, ok := sink.Find(telemetry.MetricRequests, map[string]string{"type": "completion", "outcome": "ok", "code": ""}); !ok || s.Value != 1 {
		t.Fatalf("requests = %+v (%v)", sink.Series(telemetry.MetricRequests), ok)
	}
}

func TestAModelTheProviderDoesNotListIsNeverNamed(t *testing.T) {
	rec := telemetrytest.Spans(t)
	sink := telemetrytest.NewSink()
	p := newChatFake()
	p.resp.Model = leak // a server that echoes whatever it was sent
	svc := build(t, sink, p)

	if _, err := svc.Execute(context.Background(), chatRequest(leak)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	chat := telemetrytest.One(t, rec, "chat")
	if _, ok := telemetrytest.Attrs(chat)["gen_ai.request.model"]; ok {
		t.Error("an unlisted request model was put on the span")
	}
	if _, ok := telemetrytest.Attrs(chat)["gen_ai.response.model"]; ok {
		t.Error("an unlisted response model was put on the span")
	}
	telemetrytest.AssertAbsent(t, rec, leak)
	for _, name := range []string{telemetry.MetricOperationDuration, telemetry.MetricTokenUsage} {
		for _, s := range sink.Series(name) {
			if s.Labels["gen_ai.request.model"] != "other" {
				t.Errorf("%s labelled model %q, want other", name, s.Labels["gen_ai.request.model"])
			}
		}
	}
}

func TestAFailedCallIsTypedAndNeverQuoted(t *testing.T) {
	rec := telemetrytest.Spans(t)
	sink := telemetrytest.NewSink()
	p := newChatFake()
	p.err, p.failN = errors.New("openai: API error (status 500): "+leak), 1
	svc := build(t, sink, p)

	if _, err := svc.Execute(context.Background(), chatRequest("gpt-4o")); err == nil {
		t.Fatal("want the provider's error")
	}
	chat := telemetrytest.One(t, rec, "chat gpt-4o")
	serve := telemetrytest.One(t, rec, "serve completion")
	for _, s := range []sdktrace.ReadOnlySpan{chat, serve} {
		if s.Status().Code != codes.Error || s.Status().Description != "*errors.errorString" {
			t.Errorf("%s status = %+v", s.Name(), s.Status())
		}
		if telemetrytest.Attrs(s)["error.type"] != "*errors.errorString" {
			t.Errorf("%s error.type = %q", s.Name(), telemetrytest.Attrs(s)["error.type"])
		}
	}
	wantAttrs(t, serve, map[string]string{"nexus.outcome": "error"})
	telemetrytest.AssertAbsent(t, rec, leak)
	telemetrytest.AssertAbsent(t, rec, "status 500")

	if _, ok := sink.Find(telemetry.MetricOperationDuration, map[string]string{"error.type": "*errors.errorString"}); !ok {
		t.Fatalf("no failed duration series: %+v", sink.Series(telemetry.MetricOperationDuration))
	}
	if _, ok := sink.Find(telemetry.MetricRequests, map[string]string{"outcome": "error", "code": ""}); !ok {
		t.Fatalf("no error request series: %+v", sink.Series(telemetry.MetricRequests))
	}
}

func TestEachRetryIsItsOwnProviderSpan(t *testing.T) {
	rec := telemetrytest.Spans(t)
	p := newChatFake()
	p.err, p.failN = errors.New("upstream 502"), 1
	svc := build(t, telemetrytest.NewSink(), p, middlewares.NewRetry(2, time.Millisecond, 1))

	if _, err := svc.Execute(context.Background(), chatRequest("gpt-4o")); err != nil {
		t.Fatalf("execute: %v", err)
	}
	serve := telemetrytest.One(t, rec, "serve completion")
	attempts := telemetrytest.Named(rec, "chat gpt-4o")
	if len(attempts) != 2 {
		t.Fatalf("want 2 provider spans, got %d", len(attempts))
	}
	for _, a := range attempts {
		childOf(t, a, serve)
	}
	if attempts[0].Status().Code != codes.Error || attempts[1].Status().Code == codes.Error {
		t.Fatalf("attempt statuses: %v, %v", attempts[0].Status(), attempts[1].Status())
	}
	wantAttrs(t, serve, map[string]string{"nexus.outcome": "ok"})
}

func TestAStreamSpanLastsAsLongAsTheStream(t *testing.T) {
	rec := telemetrytest.Spans(t)
	sink := telemetrytest.NewSink()
	p := newChatFake()
	p.chunks = []*provider.StreamChunk{
		{Kind: provider.EventHeartbeat},
		{ID: "chatcmpl-9", Model: "gpt-4o", Delta: provider.Delta{Content: leak}},
		{ID: "chatcmpl-9", Model: "gpt-4o", FinishReason: "stop"},
		{Kind: provider.EventUsage, Usage: &provider.Usage{PromptTokens: 5, CompletionTokens: 9, TotalTokens: 14}},
	}
	svc := build(t, sink, p)

	stream, err := svc.ExecuteStream(context.Background(), chatRequest("gpt-4o"))
	if err != nil {
		t.Fatalf("execute stream: %v", err)
	}
	if len(rec.Ended()) != 0 {
		t.Fatalf("spans ended before the stream did: %v", telemetrytest.Names(rec))
	}
	for {
		if _, err := stream.Next(context.Background()); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("next: %v", err)
		}
	}
	chat := telemetrytest.One(t, rec, "chat gpt-4o")
	if len(telemetrytest.Named(rec, "serve stream")) != 0 {
		t.Fatal("the request span ended before the stream was closed")
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	serve := telemetrytest.One(t, rec, "serve stream")
	childOf(t, chat, serve)
	wantAttrs(t, chat, map[string]string{
		"nexus.request.type": "stream", "gen_ai.response.id": "chatcmpl-9", "gen_ai.response.model": "gpt-4o",
		"gen_ai.response.finish_reasons": `["stop"]`, "gen_ai.usage.input_tokens": "5", "gen_ai.usage.output_tokens": "9",
	})
	wantAttrs(t, serve, map[string]string{"nexus.outcome": "ok", "nexus.request.type": "stream"})
	if ev := chat.Events(); len(ev) != 1 || ev[0].Name != "first chunk" {
		t.Fatalf("chat events = %+v, want one first chunk event", ev)
	}
	telemetrytest.AssertAbsent(t, rec, leak)

	if s, ok := sink.Find(telemetry.MetricFirstChunk, map[string]string{"gen_ai.provider.name": "openai", "gen_ai.request.model": "gpt-4o"}); !ok || s.Count != 1 {
		t.Fatalf("first chunk series = %+v", sink.Series(telemetry.MetricFirstChunk))
	}
	if s, ok := sink.Find(telemetry.MetricRequests, map[string]string{"type": "stream", "outcome": "ok"}); !ok || s.Value != 1 {
		t.Fatalf("requests = %+v", sink.Series(telemetry.MetricRequests))
	}
}

func TestEmbeddingsAreTracedAsEmbeddings(t *testing.T) {
	rec := telemetrytest.Spans(t)
	sink := telemetrytest.NewSink()
	svc := build(t, sink, newChatFake())

	if _, err := svc.ExecuteEmbedding(context.Background(), &provider.EmbeddingRequest{Model: "text-embedding-3-small", Input: []string{leak}}); err != nil {
		t.Fatalf("embed: %v", err)
	}
	emb := telemetrytest.One(t, rec, "embeddings text-embedding-3-small")
	childOf(t, emb, telemetrytest.One(t, rec, "serve embedding"))
	wantAttrs(t, emb, map[string]string{"gen_ai.operation.name": "embeddings", "gen_ai.usage.input_tokens": "7"})
	if _, ok := telemetrytest.Attrs(emb)["gen_ai.usage.output_tokens"]; ok {
		t.Error("an embedding has no output tokens")
	}
	telemetrytest.AssertAbsent(t, rec, leak)
	if _, ok := sink.Find(telemetry.MetricTokenUsage, map[string]string{"gen_ai.operation.name": "embeddings", "gen_ai.token.type": "input"}); !ok {
		t.Fatalf("token series = %+v", sink.Series(telemetry.MetricTokenUsage))
	}
}

// refuser refuses every request the way the quota stage does.
type refuser struct{}

func (refuser) Name() string  { return "refuser" }
func (refuser) Priority() int { return 50 }
func (refuser) Process(context.Context, *pipeline.Request, pipeline.NextFunc) (*pipeline.Response, error) {
	return nil, &pipeline.RefusalError{Code: pipeline.CodeRateLimited, Status: 429, Message: "60 requests a minute for " + leak}
}

func TestARefusalIsAnOutcomeNotAnError(t *testing.T) {
	rec := telemetrytest.Spans(t)
	sink := telemetrytest.NewSink()
	svc := build(t, sink, newChatFake(), refuser{})

	if _, err := svc.Execute(context.Background(), chatRequest("gpt-4o")); err == nil {
		t.Fatal("want the refusal")
	}
	serve := telemetrytest.One(t, rec, "serve completion")
	wantAttrs(t, serve, map[string]string{"nexus.outcome": "refused", "nexus.refusal.code": "rate_limited"})
	if serve.Status().Code == codes.Error {
		t.Fatal("a refusal is not a failure")
	}
	if len(telemetrytest.Named(rec, "chat gpt-4o")) != 0 {
		t.Fatal("a refused request reached the provider")
	}
	telemetrytest.AssertAbsent(t, rec, leak)
	if s, ok := sink.Find(telemetry.MetricRequests, map[string]string{"outcome": "refused", "code": "rate_limited"}); !ok || s.Value != 1 {
		t.Fatalf("requests = %+v", sink.Series(telemetry.MetricRequests))
	}
}

// blocker blocks every input, with a reason that quotes the prompt.
type blocker struct{}

func (blocker) Name() string       { return "pii" }
func (blocker) Phase() guard.Phase { return guard.PhaseInput }
func (blocker) Check(context.Context, *guard.CheckInput) (*guard.CheckResult, error) {
	return &guard.CheckResult{Blocked: true, Reason: "found " + leak}, nil
}

func TestAGuardrailBlockIsTracedAndCounted(t *testing.T) {
	rec := telemetrytest.Spans(t)
	sink := telemetrytest.NewSink()
	g := guard.NewService()
	g.Register(blocker{})
	svc := build(t, sink, newChatFake(), middlewares.NewGuardrail(g))

	if _, err := svc.Execute(context.Background(), chatRequest("gpt-4o")); err == nil {
		t.Fatal("want the block")
	}
	check := telemetrytest.One(t, rec, "check guardrails")
	serve := telemetrytest.One(t, rec, "serve completion")
	childOf(t, check, serve)
	wantAttrs(t, check, map[string]string{
		"nexus.guardrail.phase": "input", "nexus.guardrail.blocked": "true",
		"nexus.guardrail.modified": "false", "nexus.guardrail.name": "pii",
	})
	wantAttrs(t, serve, map[string]string{
		"nexus.outcome": "blocked", "nexus.refusal.code": "content_blocked",
		"nexus.guardrail.name": "pii", "nexus.guardrail.phase": "input",
	})
	telemetrytest.AssertAbsent(t, rec, leak)
	if s, ok := sink.Find(telemetry.MetricGuardrailBlocks, map[string]string{"phase": "input", "guard": "pii"}); !ok || s.Value != 1 {
		t.Fatalf("blocks = %+v", sink.Series(telemetry.MetricGuardrailBlocks))
	}
}

func TestCacheLookupsAreTracedAndCounted(t *testing.T) {
	rec := telemetrytest.Spans(t)
	sink := telemetrytest.NewSink()
	svc := build(t, sink, newChatFake(), middlewares.NewCache(cache.NewService(stores.NewMemory())).WithMetrics(sink))

	for range 2 {
		if _, err := svc.Execute(context.Background(), chatRequest("gpt-4o")); err != nil {
			t.Fatalf("execute: %v", err)
		}
	}
	lookups := telemetrytest.Named(rec, "look up cache")
	serves := telemetrytest.Named(rec, "serve completion")
	if len(lookups) != 2 || len(serves) != 2 {
		t.Fatalf("spans: %v", telemetrytest.Names(rec))
	}
	wantAttrs(t, lookups[0], map[string]string{"nexus.cache.kind": "response", "nexus.cache.hit": "false"})
	wantAttrs(t, lookups[1], map[string]string{"nexus.cache.kind": "response", "nexus.cache.hit": "true"})
	childOf(t, lookups[1], serves[1])
	wantAttrs(t, serves[1], map[string]string{"nexus.outcome": "cached", "nexus.cache.hit": "true"})
	if n := len(telemetrytest.Named(rec, "chat gpt-4o")); n != 1 {
		t.Fatalf("the hit called the provider: %d chat spans", n)
	}
	for result, want := range map[string]float64{"miss": 1, "hit": 1} {
		if s, ok := sink.Find(telemetry.MetricCacheLookups, map[string]string{"kind": "response", "result": result}); !ok || s.Value != want {
			t.Errorf("%s lookups = %+v", result, sink.Series(telemetry.MetricCacheLookups))
		}
	}
	if s, ok := sink.Find(telemetry.MetricRequests, map[string]string{"outcome": "cached"}); !ok || s.Value != 1 {
		t.Fatalf("requests = %+v", sink.Series(telemetry.MetricRequests))
	}
}

func TestAUsageInsertLandsInTheRequestsTrace(t *testing.T) {
	rec := telemetrytest.Spans(t)
	sink := telemetrytest.NewSink()
	usageRec := newRecordingUsage()
	usageRec.fail = errors.New("pq: duplicate key value (" + leak + ")")
	um := middlewares.NewUsage(usageRec, gpt4o, nil).WithMetrics(sink)
	svc := build(t, sink, newChatFake(), um)

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := svc.Execute(ctx, chatRequest("gpt-4o")); err != nil {
		t.Fatalf("execute: %v", err)
	}
	cancel() // the request is over; the insert must not care
	if err := um.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	store := telemetrytest.One(t, rec, "store usage record")
	childOf(t, store, telemetrytest.One(t, rec, "serve completion"))
	wantAttrs(t, store, map[string]string{"nexus.usage.outcome": "ok", "nexus.usage.pricing_status": "priced", "error.type": "*errors.errorString"})
	if store.Status().Code != codes.Error {
		t.Fatalf("failed insert status = %+v", store.Status())
	}
	telemetrytest.AssertAbsent(t, rec, leak)
	if s, ok := sink.Find(telemetry.MetricUsageInsertErrors, nil); !ok || s.Value != 1 || um.InsertErrors() != 1 {
		t.Fatalf("insert errors = %+v, InsertErrors %d", sink.Series(telemetry.MetricUsageInsertErrors), um.InsertErrors())
	}
}

func TestAUsageInsertWithoutATraceStartsNoSpan(t *testing.T) {
	rec := telemetrytest.Spans(t)
	usageRec := newRecordingUsage()
	um := middlewares.NewUsage(usageRec, gpt4o, nil)
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: chatRequest("gpt-4o"), State: map[string]any{}}
	if _, err := um.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Completion: newChatFake().resp}, nil
	}); err != nil {
		t.Fatalf("process: %v", err)
	}
	if err := um.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if len(rec.Ended()) != 0 {
		t.Fatalf("an untraced request minted spans: %v", telemetrytest.Names(rec))
	}
}

// spyLimiter records the trace each charge ran in, and can fail.
type spyLimiter struct {
	mu     sync.Mutex
	traces []trace.SpanContext
	fail   bool
}

func (l *spyLimiter) Allow(ctx context.Context, _ string, n, _ int64, _ time.Duration) (ratelimit.Decision, error) {
	if l.fail {
		return ratelimit.Decision{}, errors.New("redis: " + leak)
	}
	if n > 0 {
		l.mu.Lock()
		l.traces = append(l.traces, trace.SpanContextFromContext(ctx))
		l.mu.Unlock()
	}
	return ratelimit.Decision{Allowed: true}, nil
}
func (*spyLimiter) Kind() string { return "spy" }

func TestTheTokenChargeKeepsTheRequestsTrace(t *testing.T) {
	telemetrytest.Spans(t)
	lim := &spyLimiter{}
	qm := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: lim})
	ctx, span := telemetry.Tracer().Start(context.Background(), "request")
	ctx = middlewares.WithTenantForTest(ctx, quotaTenant(tenant.Quota{TPM: 1000}))
	ctx, cancel := context.WithCancel(ctx)
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{}, State: map[string]any{}}
	if _, err := qm.Process(ctx, req, func(context.Context) (*pipeline.Response, error) {
		cancel()
		return &pipeline.Response{Completion: &provider.CompletionResponse{Usage: provider.Usage{TotalTokens: 100}}}, nil
	}); err != nil {
		t.Fatalf("process: %v", err)
	}
	span.End()
	if len(lim.traces) != 1 || lim.traces[0].TraceID() != span.SpanContext().TraceID() {
		t.Fatalf("charge traces = %v, want the request's %v", lim.traces, span.SpanContext().TraceID())
	}
}

func TestLimiterFailuresAreCounted(t *testing.T) {
	sink := telemetrytest.NewSink()
	qm := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: &spyLimiter{fail: true}, GlobalRPM: 10, Metrics: sink})
	if err := runQuota(t, qm, quotaTenant(tenant.Quota{}), &provider.CompletionRequest{}); err != nil {
		t.Fatalf("a failing limiter must let the request through: %v", err)
	}
	if s, ok := sink.Find(telemetry.MetricLimiterErrors, nil); !ok || s.Value != 1 || qm.LimiterErrors() != 1 {
		t.Fatalf("limiter errors = %+v, LimiterErrors %d", sink.Series(telemetry.MetricLimiterErrors), qm.LimiterErrors())
	}
}

// spyStreamCache records the trace a stream was written in.
type spyStreamCache struct {
	mu    sync.Mutex
	trace trace.SpanContext
	sets  int
}

func (*spyStreamCache) GetStream(context.Context, string) ([]cache.StreamFrame, error) {
	return nil, nil
}
func (c *spyStreamCache) SetStream(ctx context.Context, _ string, _ []cache.StreamFrame, _ time.Duration) error {
	c.mu.Lock()
	c.trace, c.sets = trace.SpanContextFromContext(ctx), c.sets+1
	c.mu.Unlock()
	return nil
}
func (*spyStreamCache) DeleteStream(context.Context, string) error { return nil }

func TestAStreamCacheWriteKeepsTheRequestsTrace(t *testing.T) {
	rec := telemetrytest.Spans(t)
	sink := telemetrytest.NewSink()
	sc := &spyStreamCache{}
	p := newChatFake()
	p.chunks = []*provider.StreamChunk{{Delta: provider.Delta{Content: "hi"}}}
	svc := build(t, sink, p, middlewares.NewCache(nil).WithStreamCache(sc, cache.StreamCacheOptions{}).WithMetrics(sink))

	stream, err := svc.ExecuteStream(context.Background(), chatRequest("gpt-4o"))
	if err != nil {
		t.Fatalf("execute stream: %v", err)
	}
	for {
		if _, err := stream.Next(context.Background()); err != nil {
			break
		}
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	serve := telemetrytest.One(t, rec, "serve stream")
	if sc.sets != 1 || sc.trace.TraceID() != serve.SpanContext().TraceID() {
		t.Fatalf("stream cache write: %d sets, trace %v, want the request's %v", sc.sets, sc.trace.TraceID(), serve.SpanContext().TraceID())
	}
	lookup := telemetrytest.One(t, rec, "look up cache")
	wantAttrs(t, lookup, map[string]string{"nexus.cache.kind": "stream", "nexus.cache.hit": "false"})
	if _, ok := sink.Find(telemetry.MetricCacheLookups, map[string]string{"kind": "stream", "result": "miss"}); !ok {
		t.Fatalf("lookups = %+v", sink.Series(telemetry.MetricCacheLookups))
	}
}

// Every series under one name carries the same label keys, whatever the
// outcome, so a dashboard can aggregate them.
func TestEverySeriesOfAMetricHasTheSameLabelKeys(t *testing.T) {
	telemetrytest.Spans(t)
	sink := telemetrytest.NewSink()
	ok := newChatFake()
	failing := newChatFake()
	failing.err, failing.failN = errors.New("boom"), 1
	for _, p := range []*chatFake{ok, failing} {
		svc := build(t, sink, p)
		_, _ = svc.Execute(context.Background(), chatRequest("gpt-4o"))
		_, _ = svc.Execute(context.Background(), chatRequest(leak))
	}
	for _, name := range sink.Names() {
		var keys string
		for i, s := range sink.Series(name) {
			k := labelKeys(s.Labels)
			if i == 0 {
				keys = k
			} else if k != keys {
				t.Errorf("%s: label keys %q and %q", name, keys, k)
			}
		}
	}
}

func labelKeys(m map[string]string) string {
	return strings.Join(slices.Sorted(maps.Keys(m)), ",")
}

func TestTheProviderIsCalledWithoutTheClientsBaggage(t *testing.T) {
	telemetrytest.Spans(t)
	p := newChatFake()
	svc := build(t, telemetrytest.NewSink(), p)
	member, err := baggage.NewMember("user", "alice")
	if err != nil {
		t.Fatal(err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatal(err)
	}
	ctx, server := parent(t)
	ctx = baggage.ContextWithBaggage(ctx, bag)
	if _, err := svc.Execute(ctx, chatRequest("gpt-4o")); err != nil {
		t.Fatalf("execute: %v", err)
	}
	server.End()
	if p.baggage != 0 {
		t.Fatalf("the provider was called with %d baggage members, want none", p.baggage)
	}
	if !p.gotSpan.IsValid() {
		t.Fatal("the provider lost the trace context along with the baggage")
	}
}

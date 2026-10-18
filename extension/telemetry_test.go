package extension

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge"

	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
)

// httpProvider calls an upstream over HTTP with a client it owns, the way the
// providers in providers/ do, and lets the gateway wrap its transport.
type httpProvider struct {
	url    string
	client *http.Client
}

func (p *httpProvider) Name() string { return "openai" }
func (p *httpProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Chat: true}
}
func (p *httpProvider) Models(context.Context) ([]provider.Model, error) {
	return []provider.Model{{ID: "gpt-4o"}}, nil
}
func (p *httpProvider) Complete(ctx context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out provider.CompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
func (p *httpProvider) CompleteStream(context.Context, *provider.CompletionRequest) (provider.Stream, error) {
	return nil, provider.ErrNotSupported
}
func (p *httpProvider) Embed(context.Context, *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, provider.ErrNotSupported
}
func (p *httpProvider) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, http.NoBody)
	if err != nil {
		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}
func (p *httpProvider) WrapTransport(wrap func(http.RoundTripper) http.RoundTripper) {
	p.client.Transport = wrap(p.client.Transport)
}

func scrape(t *testing.T, app forge.App) string {
	t.Helper()
	p, ok := app.Metrics().(interface{ PrometheusHandler() http.Handler })
	if !ok {
		t.Fatalf("app metrics %T have no Prometheus handler", app.Metrics())
	}
	rr := httptest.NewRecorder()
	p.PrometheusHandler().ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/_/metrics", nil))
	body, err := io.ReadAll(rr.Body)
	if err != nil {
		t.Fatalf("read scrape: %v", err)
	}
	return string(body)
}

func TestExtensionWiresTelemetryIntoForge(t *testing.T) {
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	app := forge.New(forge.WithAppName("nexus-telemetry-test"), forge.WithAppLogger(forge.NewNoopLogger()))
	rec := tracetest.NewSpanRecorder()
	if err := forge.RegisterSpanProcessor(app, rec); err != nil {
		t.Fatalf("RegisterSpanProcessor: %v", err)
	}

	// Propagate baggage as well as the trace, the way forge's defaults do, so
	// the test would see a Baggage header if the gateway let one through.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	var upstreamTrace trace.SpanContext
	var upstreamBaggage []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamBaggage = append(upstreamBaggage, r.Header.Get("Baggage"))
		if r.Method == http.MethodGet {
			return // the health check
		}
		upstreamTrace = trace.SpanContextFromContext(otel.GetTextMapPropagator().Extract(r.Context(), propagationCarrier(r.Header)))
		_ = json.NewEncoder(w).Encode(provider.CompletionResponse{
			ID: "chatcmpl-1", Model: "gpt-4o",
			Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "hi"}, FinishReason: "stop"}},
			Usage:   provider.Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
		})
	}))
	t.Cleanup(upstream.Close)

	ext := New(WithDatabase(store.NewMemory()), WithProvider(&httpProvider{url: upstream.URL, client: &http.Client{}}))
	if err := ext.Register(app); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := ext.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = ext.Stop(context.Background()) })
	gw := ext.Gateway()

	tn, err := gw.Tenants().Create(context.Background(), &tenant.CreateInput{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	k, _, err := gw.Keys().Create(context.Background(), &key.CreateInput{TenantID: tn.ID.String(), Name: "k"})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	// The client sent baggage that names its user; the server middleware has
	// put it on the request context.
	member, err := baggage.NewMember("user.id", "alice")
	if err != nil {
		t.Fatal(err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatal(err)
	}
	ctx, server := forge.StartSpan(baggage.ContextWithBaggage(t.Context(), bag), "POST /v1/chat/completions", trace.WithSpanKind(trace.SpanKindServer))
	if _, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{
		Model: "gpt-4o", TenantID: tn.ID.String(), KeyID: k.ID.String(),
		Messages: []provider.Message{{Role: "user", Content: "alice@example.com"}},
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	// A health check runs on an admin request's context, outside the pipeline.
	for _, p := range gw.Providers().All() {
		p.Healthy(ctx)
	}
	server.End()
	if len(upstreamBaggage) != 2 {
		t.Fatalf("upstream saw %d requests, want the completion and the health check", len(upstreamBaggage))
	}
	for i, b := range upstreamBaggage {
		if b != "" {
			t.Errorf("request %d to the provider carried Baggage %q", i, b)
		}
	}
	if err := gw.FlushUsage(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// The spans nest: server, pipeline, provider call, outbound HTTP.
	byName := map[string]trace.SpanContext{}
	parents := map[string]trace.SpanID{}
	var httpSpan string
	for _, s := range rec.Ended() {
		byName[s.Name()] = s.SpanContext()
		parents[s.Name()] = s.Parent().SpanID()
		if strings.Contains(s.InstrumentationScope().Name, "otelhttp") && s.SpanKind() == trace.SpanKindClient && s.Name() == "HTTP POST" {
			httpSpan = s.Name()
		}
	}
	for child, parent := range map[string]string{
		"serve completion":   "POST /v1/chat/completions",
		"chat gpt-4o":        "serve completion",
		"store usage record": "serve completion",
	} {
		if _, ok := byName[child]; !ok {
			t.Fatalf("no %q span; got %v", child, parents)
		}
		if parents[child] != byName[parent].SpanID() {
			t.Errorf("%q is not a child of %q", child, parent)
		}
	}
	if httpSpan == "" || parents[httpSpan] != byName["chat gpt-4o"].SpanID() {
		t.Fatalf("the outbound call has no HTTP client span under the provider span (http span %q)", httpSpan)
	}
	if upstreamTrace.TraceID() != byName["chat gpt-4o"].TraceID() {
		t.Fatal("the outbound call did not carry the trace headers")
	}

	// The metrics are on the app's scrape. gen_ai names are outside forge's
	// semconv namespaces, so they carry the forge_ prefix and no unit suffix.
	body := scrape(t, app)
	for _, want := range []string{
		`forge_gen_ai_client_operation_duration_count{error_type="",gen_ai_operation_name="chat",gen_ai_provider_name="openai",gen_ai_request_model="gpt-4o"} 1`,
		`forge_gen_ai_client_token_usage_sum{gen_ai_operation_name="chat",gen_ai_provider_name="openai",gen_ai_request_model="gpt-4o",gen_ai_token_type="input"} 3`,
		`forge_gen_ai_client_token_usage_sum{gen_ai_operation_name="chat",gen_ai_provider_name="openai",gen_ai_request_model="gpt-4o",gen_ai_token_type="output"} 4`,
		`forge_nexus_requests_total{code="",outcome="ok",type="completion"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape lacks %s", want)
		}
	}
	if strings.Contains(body, "alice@example.com") {
		t.Error("a prompt reached the scrape")
	}
	if t.Failed() {
		var lines []string
		for _, l := range strings.Split(body, "\n") {
			if strings.Contains(l, "gen_ai") || strings.Contains(l, "nexus_") {
				lines = append(lines, l)
			}
		}
		t.Logf("nexus series on the scrape:\n%s", strings.Join(lines, "\n"))
	}
}

// propagationCarrier reads trace headers off an HTTP request.
type propagationCarrier http.Header

func (c propagationCarrier) Get(k string) string { return http.Header(c).Get(k) }
func (c propagationCarrier) Set(k, v string)     { http.Header(c).Set(k, v) }
func (c propagationCarrier) Keys() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	return out
}

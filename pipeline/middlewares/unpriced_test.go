package middlewares_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/internal/telemetry"
	"github.com/xraph/nexus/internal/telemetry/telemetrytest"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/usage"
)

// pricedRouted is a routed provider whose models carry list prices.
type pricedRouted struct {
	*routed
	prices map[string]provider.Pricing
}

func (p *pricedRouted) Models(context.Context) ([]provider.Model, error) {
	out := make([]provider.Model, 0, len(p.prices))
	for modelID, price := range p.prices {
		out = append(out, provider.Model{ID: modelID, Provider: p.name, Pricing: price})
	}
	return out, nil
}

// unpricedPipeline is usage recording on rec, then the provider call over p
// with policy. The price book reads p's list.
func unpricedPipeline(t *testing.T, rec usage.Service, p provider.Provider, policy middlewares.UnpricedPolicy) pipeline.Service {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(p)
	book := model.NewPriceBook(reg)
	svc, err := pipeline.NewBuilder().
		Use(middlewares.NewUsage(rec, book, nil)).
		Use(middlewares.NewProviderCall(nil, reg).WithModelCatalog(book).WithUnpricedPolicy(book, policy)).
		Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return svc
}

// call sends one request of kind to svc for modelID.
func call(ctx context.Context, svc pipeline.Service, kind pipeline.RequestType, modelID string) error {
	switch kind {
	case pipeline.RequestStream:
		_, err := svc.ExecuteStream(ctx, &provider.CompletionRequest{Model: modelID, Stream: true})
		return err
	case pipeline.RequestEmbedding:
		_, err := svc.ExecuteEmbedding(ctx, &provider.EmbeddingRequest{Model: modelID, Input: []string{"hi"}})
		return err
	default:
		_, err := svc.Execute(ctx, &provider.CompletionRequest{Model: modelID})
		return err
	}
}

func TestUnpricedPolicyRefusesBeforeTheCall(t *testing.T) {
	for _, kind := range []pipeline.RequestType{pipeline.RequestCompletion, pipeline.RequestStream, pipeline.RequestEmbedding} {
		t.Run(string(kind), func(t *testing.T) {
			p := newRouted("openai", true)
			rec := newRecordingUsage()
			svc := unpricedPipeline(t, rec, p, middlewares.RefuseUnpriced)
			ctx := pipeline.WithRequestID(context.Background(), id.NewRequestID().String())

			err := call(ctx, svc, kind, "o9")
			if !errors.Is(err, middlewares.ErrModelUnpriced) {
				t.Fatalf("err = %v, want ErrModelUnpriced", err)
			}
			if status, code := pipeline.HTTPStatus(err); status != 400 || code != "model_unpriced" {
				t.Fatalf("status %d code %q, want 400 model_unpriced", status, code)
			}
			if p.Calls() != 0 {
				t.Fatalf("the provider was called %d times; an unpriced model must be refused before the call", p.Calls())
			}
			<-rec.done
			got := rec.only(t)
			if got.Outcome != usage.OutcomeRefused || got.PricingStatus != usage.PricingNotCharged || got.RefusalCode != "model_unpriced" {
				t.Fatalf("record = %s/%s code %q, want refused/not_charged model_unpriced", got.Outcome, got.PricingStatus, got.RefusalCode)
			}
		})
	}
}

func TestUnpricedPolicyIsNotAskedAboutAPricedModel(t *testing.T) {
	p := &pricedRouted{routed: newRouted("openai", true), prices: map[string]provider.Pricing{
		"gpt-4o": {InputPerMillion: money.MustParse("2.50"), OutputPerMillion: money.MustParse("10")},
	}}
	asked := 0
	policy := func(context.Context, string, string, string) error { asked++; return middlewares.ErrModelUnpriced }
	svc := unpricedPipeline(t, newRecordingUsage(), p, policy)
	if err := call(context.Background(), svc, pipeline.RequestCompletion, "gpt-4o"); err != nil {
		t.Fatalf("a priced model was refused: %v", err)
	}
	if asked != 0 || p.Calls() != 1 {
		t.Fatalf("policy asked %d times, provider called %d; want 0 and 1", asked, p.Calls())
	}
}

func TestAnEmbeddingModelPricedOnlyForEmbeddingsIsNotRefused(t *testing.T) {
	p := &pricedRouted{routed: newRouted("openai", true), prices: map[string]provider.Pricing{
		"text-embedding-3-small": {EmbeddingPerMillion: money.MustParse("0.02")},
	}}
	svc := unpricedPipeline(t, newRecordingUsage(), p, middlewares.RefuseUnpriced)
	if err := call(context.Background(), svc, pipeline.RequestEmbedding, "text-embedding-3-small"); err != nil {
		t.Fatalf("an embedding to a model with an embedding price was refused: %v", err)
	}
	if p.Calls() != 1 {
		t.Fatalf("provider called %d times, want 1", p.Calls())
	}
	// The same model has no price for a completion, so a completion is refused.
	if err := call(context.Background(), svc, pipeline.RequestCompletion, "text-embedding-3-small"); !errors.Is(err, middlewares.ErrModelUnpriced) {
		t.Fatalf("completion err = %v, want ErrModelUnpriced", err)
	}
	if p.Calls() != 1 {
		t.Fatalf("provider called %d times after the refused completion, want 1", p.Calls())
	}
}

func TestUnpricedPolicySeesTenantProviderAndModel(t *testing.T) {
	p := newRouted("openai", true)
	tenant := id.NewTenantID().String()
	var mu sync.Mutex
	var seen [][3]string
	policy := func(_ context.Context, tenantID, providerName, modelID string) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, [3]string{tenantID, providerName, modelID})
		return nil
	}
	svc := unpricedPipeline(t, newRecordingUsage(), p, policy)
	ctx := pipeline.WithTenantID(context.Background(), tenant)
	if err := call(ctx, svc, pipeline.RequestCompletion, "gpt-x"); err != nil {
		t.Fatalf("a policy that returns nil should let the call through: %v", err)
	}
	if p.Calls() != 1 {
		t.Fatalf("provider called %d times, want 1", p.Calls())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != [3]string{tenant, "openai", "gpt-x"} {
		t.Fatalf("policy saw %v, want one call with (%s, openai, gpt-x)", seen, tenant)
	}
}

func TestAPlainPolicyErrorIsPermanentAndNotCharged(t *testing.T) {
	p := newRouted("openai", true)
	rec := newRecordingUsage()
	svc := unpricedPipeline(t, rec, p, func(context.Context, string, string, string) error { return errors.New("pricing service down") })
	err := call(context.Background(), svc, pipeline.RequestCompletion, "gpt-x")
	if err == nil || !pipeline.IsPermanent(err) {
		t.Fatalf("err = %v, want a permanent error", err)
	}
	if p.Calls() != 0 {
		t.Fatalf("provider called %d times, want 0", p.Calls())
	}
	<-rec.done
	if got := rec.only(t); got.Outcome != usage.OutcomeError || got.PricingStatus != usage.PricingNotCharged {
		t.Fatalf("record = %s/%s, want error/not_charged: no provider was called", got.Outcome, got.PricingStatus)
	}
}

// warnLog counts warnings by message and keeps their arguments.
type warnLog struct {
	mu    sync.Mutex
	warns [][]any
}

func (l *warnLog) Error(string, ...any) {}
func (l *warnLog) Warn(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, append([]any{msg}, args...))
}

func TestUnpricedUsageIsCountedAndLoggedOnce(t *testing.T) {
	rec := newRecordingUsage()
	log := &warnLog{}
	sink := telemetrytest.NewSink()
	mw := middlewares.NewUsage(rec, prices{}, log).WithMetrics(sink)
	served := func(modelID string) {
		req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{Model: modelID}, State: map[string]any{}}
		_, _ = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
			req.State[pipeline.StateProviderName] = "p"
			return &pipeline.Response{Completion: &provider.CompletionResponse{Model: modelID, Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 5}}}, nil
		})
	}
	served("llama3")
	served("llama3")
	if err := mw.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if s, ok := sink.Find(telemetry.MetricUsageUnpriced, map[string]string{"provider": "p"}); !ok || s.Value != 2 {
		t.Fatalf("nexus_usage_unpriced_total{provider=p} = %+v (%v), want 2", sink.Series(telemetry.MetricUsageUnpriced), ok)
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if len(log.warns) != 1 {
		t.Fatalf("warned %d times, want once per (provider, model): %v", len(log.warns), log.warns)
	}
	want := []any{"nexus: usage for a model with no price; its cost is unknown", "provider", "p", "model", "llama3"}
	for i, v := range want {
		if log.warns[0][i] != v {
			t.Fatalf("warning = %v, want %v", log.warns[0], want)
		}
	}
}

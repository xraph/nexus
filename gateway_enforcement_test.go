package nexus_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/ratelimit"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// enforced builds a gateway over s with one fake provider, creates a tenant
// with quota q and a key for it, and returns them.
func enforced(t *testing.T, s store.Store, q tenant.Quota, opts ...nexus.Option) (*nexus.Gateway, *tenant.Tenant, *key.APIKey) {
	t.Helper()
	return enforcedWith(t, s, q, &fakeProvider{name: "openai", price: listPrice}, opts...)
}

// enforcedWith is enforced over a provider the test holds.
func enforcedWith(t *testing.T, s store.Store, q tenant.Quota, p *fakeProvider, opts ...nexus.Option) (*nexus.Gateway, *tenant.Tenant, *key.APIKey) {
	t.Helper()
	gw := nexus.New(append([]nexus.Option{nexus.WithDatabase(s), nexus.WithProvider(p)}, opts...)...)
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gw.Shutdown(context.Background()) })
	tn, err := gw.Tenants().Create(context.Background(), &tenant.CreateInput{Name: "Acme", Slug: "acme-" + t.Name(), Quota: &q})
	if err != nil {
		t.Fatal(err)
	}
	k, _, err := gw.Keys().Create(context.Background(), &key.CreateInput{TenantID: tn.ID.String(), Name: "k"})
	if err != nil {
		t.Fatal(err)
	}
	return gw, tn, k
}

func complete(gw *nexus.Gateway, tn *tenant.Tenant, k *key.APIKey) error {
	_, err := gw.Engine().Complete(context.Background(), &provider.CompletionRequest{
		Model: "gpt-4o", TenantID: tn.ID.String(), KeyID: k.ID.String(),
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	return err
}

func refusedCode(err error) string { _, c := pipeline.HTTPStatus(err); return c }

// records flushes the gateway's usage and returns every stored record.
func records(t *testing.T, gw *nexus.Gateway, s store.Store) []*usage.Record {
	t.Helper()
	if err := gw.FlushUsage(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	res, err := s.Usage().Query(context.Background(), &usage.QueryOptions{Limit: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	return res.Items
}

// fixedClock is a limiter clock that never reaches a minute boundary, so a
// test cannot flake when the wall clock rolls over mid-run.
func fixedClock() ratelimit.Limiter {
	fixed := time.Date(2026, 10, 7, 12, 0, 30, 0, time.UTC)
	return ratelimit.NewMemory(ratelimit.WithClock(func() time.Time { return fixed }))
}

func TestTheBudgetIsASoftLimit(t *testing.T) {
	s := store.NewMemory()
	// One completion costs 0.008755. A 0.01 budget lets two through: the
	// second crosses it and completes; the third is refused.
	gw, tn, k := enforced(t, s, tenant.Quota{MonthlyBudgetUSD: money.MustParse("0.01")})
	for i := 1; i <= 2; i++ {
		if err := complete(gw, tn, k); err != nil {
			t.Fatalf("request %d = %v", i, err)
		}
		if err := gw.FlushUsage(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := complete(gw, tn, k); refusedCode(err) != pipeline.CodeBudgetExceeded {
		t.Fatalf("third request = %v; want budget_exceeded", err)
	}
	var refused int
	for _, r := range records(t, gw, s) {
		if r.Outcome == usage.OutcomeRefused {
			refused++
			if r.RefusalCode != pipeline.CodeBudgetExceeded || r.CostUSD == nil || !r.CostUSD.IsZero() || r.TenantID != tn.ID {
				t.Fatalf("refusal record = %+v", r)
			}
		}
	}
	if refused != 1 {
		t.Fatalf("refused records = %d", refused)
	}
}

func TestADisabledTenantIsRefusedAndRecorded(t *testing.T) {
	s := store.NewMemory()
	gw, tn, k := enforced(t, s, tenant.Quota{})
	if err := gw.Tenants().SetStatus(context.Background(), tn.ID.String(), tenant.StatusDisabled); err != nil {
		t.Fatal(err)
	}
	if err := complete(gw, tn, k); refusedCode(err) != pipeline.CodeForbidden {
		t.Fatalf("disabled tenant = %v", err)
	}
	recs := records(t, gw, s)
	if len(recs) != 1 {
		t.Fatalf("records = %+v; want exactly one", recs)
	}
	r := recs[0]
	if r.Outcome != usage.OutcomeRefused || r.RefusalCode != pipeline.CodeForbidden || r.TenantID != tn.ID || r.CostUSD == nil || !r.CostUSD.IsZero() {
		t.Fatalf("record = outcome %s code %q tenant %s cost %v; want a $0 forbidden refusal charged to the tenant", r.Outcome, r.RefusalCode, r.TenantID, r.CostUSD)
	}
}

func TestRateLimitRefusalsDoNotUseUpTheDailyQuota(t *testing.T) {
	s := store.NewMemory()
	gw, tn, k := enforced(t, s, tenant.Quota{RPM: 1, DailyRequests: 2}, nexus.WithLimiter(fixedClock()))
	_ = complete(gw, tn, k)
	for i := 0; i < 5; i++ {
		if err := complete(gw, tn, k); refusedCode(err) != pipeline.CodeRateLimited {
			t.Fatalf("over RPM = %v", err)
		}
	}
	if err := gw.FlushUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	n, err := gw.Usage().DailyRequests(context.Background(), tn.ID.String())
	if err != nil || n != 1 {
		t.Fatalf("daily requests = %d, %v; five rate-limit refusals must not count", n, err)
	}
}

func TestAnUnknownTenantIsRefusedAndChargedToNoOne(t *testing.T) {
	s := store.NewMemory()
	gw, _, _ := enforced(t, s, tenant.Quota{})
	ghost := id.NewTenantID().String()
	_, err := gw.Engine().Complete(context.Background(), &provider.CompletionRequest{Model: "gpt-4o", TenantID: ghost})
	if refusedCode(err) != pipeline.CodeForbidden {
		t.Fatalf("unknown tenant = %v", err)
	}
	recs := records(t, gw, s)
	if len(recs) != 1 || !recs[0].TenantID.IsNil() {
		t.Fatalf("records = %+v; want one, unattributed", recs)
	}
}

type brokenLimiter struct{}

func (brokenLimiter) Allow(context.Context, string, int64, int64, time.Duration) (ratelimit.Decision, error) {
	return ratelimit.Decision{}, errors.New("redis: connection refused")
}
func (brokenLimiter) Kind() string { return "redis" }

func TestTheGatewayCountsLimiterFailuresAndServes(t *testing.T) {
	gw, tn, k := enforced(t, store.NewMemory(), tenant.Quota{RPM: 1}, nexus.WithLimiter(brokenLimiter{}))
	for i := 0; i < 3; i++ {
		if err := complete(gw, tn, k); err != nil {
			t.Fatalf("request %d with the limiter down = %v", i, err)
		}
	}
	if gw.LimiterErrors() != 3 || gw.Limiter().Kind() != "redis" {
		t.Fatalf("limiter errors = %d, kind %s", gw.LimiterErrors(), gw.Limiter().Kind())
	}
}

func TestTheGlobalRateLimitIsEnforced(t *testing.T) {
	s := store.NewMemory()
	gw := nexus.New(nexus.WithDatabase(s), nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice}),
		nexus.WithRateLimit(1), nexus.WithLimiter(fixedClock()))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gw.Shutdown(context.Background()) })
	do := func() error {
		_, err := gw.Engine().Complete(context.Background(), &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}})
		return err
	}
	if err := do(); err != nil {
		t.Fatalf("first request = %v", err)
	}
	if err := do(); refusedCode(err) != pipeline.CodeRateLimited {
		t.Fatalf("second unattributed request = %v; want rate_limited", err)
	}
}

func TestTheTenantsStreamLimitCutsAStreamByDefault(t *testing.T) {
	s := store.NewMemory()
	over := &provider.Usage{PromptTokens: 10, CompletionTokens: 50, TotalTokens: 60}
	p := &fakeProvider{name: "openai", price: listPrice, stream: []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "he"}}, {Kind: provider.EventUsage, Usage: over},
	}}
	gw, tn, k := enforcedWith(t, s, tenant.Quota{MaxStreamTokens: 20}, p)
	ctx := context.Background()
	st, err := gw.Engine().CompleteStream(ctx, &provider.CompletionRequest{Model: "gpt-4o", Stream: true, TenantID: tn.ID.String(), KeyID: k.ID.String()})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	closed := false
	closeStream := func() {
		if !closed {
			closed = true
			_ = st.Close()
		}
	}
	// A failed assertion must not leave the stream open: Shutdown waits for it.
	defer closeStream()
	var cut error
	for {
		if _, cut = st.Next(ctx); cut != nil {
			break
		}
	}
	if code := refusedCode(cut); code != pipeline.CodeQuotaExceeded {
		t.Fatalf("stream ended with %v; want quota_exceeded", cut)
	}
	closeStream()
	recs := records(t, gw, s)
	if len(recs) != 1 {
		t.Fatalf("records = %+v; want one", recs)
	}
	if r := recs[0]; r.StatusCode != 429 || r.RefusalCode != pipeline.CodeQuotaExceeded || r.TenantID != tn.ID {
		t.Fatalf("record = status %d code %q tenant %s; want 429 quota_exceeded for the tenant", r.StatusCode, r.RefusalCode, r.TenantID)
	}
}

// pausingProvider answers every completion after a pause, so requests sent
// together are all in flight at once. It is safe for concurrent use.
type pausingProvider struct {
	fakeProvider
	delay  time.Duration
	served atomic.Int64
}

func (p *pausingProvider) Complete(_ context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	time.Sleep(p.delay)
	p.served.Add(1)
	return &provider.CompletionResponse{Provider: p.name, Model: req.Model,
		Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "hello"}}},
		Usage:   provider.Usage{PromptTokens: 1234, CompletionTokens: 567, TotalTokens: 1801}}, nil
}

// burst sends n completions at the same moment and returns how many were
// served and the refusal code of each one that was not.
func burst(gw *nexus.Gateway, tn *tenant.Tenant, k *key.APIKey, n int) (served int, codes []string) {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		start = make(chan struct{})
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := complete(gw, tn, k)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				served++
				return
			}
			codes = append(codes, refusedCode(err))
		}()
	}
	close(start)
	wg.Wait()
	return served, codes
}

// slowGateway is enforced over a pausingProvider with a 20ms pause and the
// fixed-clock limiter.
func slowGateway(t *testing.T, q tenant.Quota) (*nexus.Gateway, *tenant.Tenant, *key.APIKey, *pausingProvider) {
	t.Helper()
	p := &pausingProvider{fakeProvider: fakeProvider{name: "openai", price: listPrice}, delay: 20 * time.Millisecond}
	gw := nexus.New(nexus.WithDatabase(store.NewMemory()), nexus.WithProvider(p), nexus.WithLimiter(fixedClock()))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gw.Shutdown(context.Background()) })
	tn, err := gw.Tenants().Create(context.Background(), &tenant.CreateInput{Name: "Acme", Slug: "acme", Quota: &q})
	if err != nil {
		t.Fatal(err)
	}
	k, _, err := gw.Keys().Create(context.Background(), &key.CreateInput{TenantID: tn.ID.String(), Name: "k"})
	if err != nil {
		t.Fatal(err)
	}
	return gw, tn, k, p
}

func TestTheDailyCapHoldsUnderConcurrency(t *testing.T) {
	gw, tn, k, p := slowGateway(t, tenant.Quota{DailyRequests: 5})
	served, codes := burst(gw, tn, k, 50)
	if served != 5 || p.served.Load() != 5 {
		t.Fatalf("served %d (provider saw %d) of 50 parallel requests; a 5 a day cap serves exactly 5", served, p.served.Load())
	}
	for _, c := range codes {
		if c != pipeline.CodeQuotaExceeded {
			t.Fatalf("refusal code %q; want quota_exceeded for every request over the cap", c)
		}
	}
}

// The budget is soft. Nothing is reserved before a request runs, so every
// request admitted while the stored spend is under the budget is served.
// This pins the bound the docs state: more than one, and never more than the
// requests in flight. It documents the behaviour; it is not a cap.
func TestTheBudgetOvershootIsBoundedByTheRequestsInFlight(t *testing.T) {
	const inFlight = 20
	// One completion costs 0.008755, so a 0.01 budget serves two in a row.
	gw, tn, k, _ := slowGateway(t, tenant.Quota{MonthlyBudgetUSD: money.MustParse("0.01")})
	served, _ := burst(gw, tn, k, inFlight)
	if served <= 1 || served > inFlight {
		t.Fatalf("served %d of %d parallel requests on a $0.01 budget; want more than 1 and at most %d", served, inFlight, inFlight)
	}
	t.Logf("served %d of %d parallel requests on a $0.01 budget", served, inFlight)
}

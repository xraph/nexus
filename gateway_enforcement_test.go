package nexus_test

import (
	"context"
	"errors"
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
	gw := nexus.New(append([]nexus.Option{nexus.WithDatabase(s), nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice})}, opts...)...)
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
	_ = gw.FlushUsage(context.Background())
	res, _ := s.Usage().Query(context.Background(), &usage.QueryOptions{Limit: 10})
	var refused int
	for _, r := range res.Items {
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
}

func TestRateLimitRefusalsDoNotUseUpTheDailyQuota(t *testing.T) {
	s := store.NewMemory()
	gw, tn, k := enforced(t, s, tenant.Quota{RPM: 1, DailyRequests: 2})
	_ = complete(gw, tn, k)
	for i := 0; i < 5; i++ {
		if err := complete(gw, tn, k); refusedCode(err) != pipeline.CodeRateLimited {
			t.Fatalf("over RPM = %v", err)
		}
	}
	_ = gw.FlushUsage(context.Background())
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
	_ = gw.FlushUsage(context.Background())
	res, _ := s.Usage().Query(context.Background(), &usage.QueryOptions{Limit: 10})
	if len(res.Items) != 1 || !res.Items[0].TenantID.IsNil() {
		t.Fatalf("records = %+v; want one, unattributed", res.Items)
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

package middlewares_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/ratelimit"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/testutil"
)

type counter struct {
	daily int
	spend money.USD
	err   error
}

func (c *counter) DailyRequests(context.Context, string) (int, error)      { return c.daily, c.err }
func (c *counter) MonthlySpend(context.Context, string) (money.USD, error) { return c.spend, c.err }

type budgetEvents struct {
	mu       sync.Mutex
	warned   int
	exceeded int
}

func (b *budgetEvents) EmitBudgetWarning(context.Context, id.TenantID, float64) {
	b.mu.Lock()
	b.warned++
	b.mu.Unlock()
}

func (b *budgetEvents) EmitBudgetExceeded(context.Context, id.TenantID) {
	b.mu.Lock()
	b.exceeded++
	b.mu.Unlock()
}

type brokenLimiter struct{}

func (brokenLimiter) Allow(context.Context, string, int64, int64, time.Duration) (ratelimit.Decision, error) {
	return ratelimit.Decision{}, errors.New("redis: connection refused")
}
func (brokenLimiter) Kind() string { return "redis" }

func quotaTenant(q tenant.Quota) *tenant.Tenant {
	return &tenant.Tenant{ID: id.NewTenantID(), Status: tenant.StatusActive, Quota: q}
}

func runQuota(t *testing.T, mw *middlewares.QuotaMiddleware, tn *tenant.Tenant, req *provider.CompletionRequest) error {
	t.Helper()
	ctx := middlewares.WithTenantForTest(context.Background(), tn)
	_, err := mw.Process(ctx, &pipeline.Request{Type: pipeline.RequestCompletion, Completion: req, State: map[string]any{}},
		func(context.Context) (*pipeline.Response, error) {
			return &pipeline.Response{Completion: &provider.CompletionResponse{Usage: provider.Usage{TotalTokens: 100}}}, nil
		})
	return err
}

func TestMaxTokensIsCappedAndFilledIn(t *testing.T) {
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: ratelimit.NewMemory()})
	tn := quotaTenant(tenant.Quota{MaxTokensPerReq: 500})
	if err := runQuota(t, mw, tn, &provider.CompletionRequest{MaxTokens: 501}); code(err) != pipeline.CodeInvalidRequest {
		t.Fatalf("over the cap = %v", err)
	}
	req := &provider.CompletionRequest{}
	if err := runQuota(t, mw, tn, req); err != nil || req.MaxTokens != 500 {
		t.Fatalf("unset max_tokens = %d, %v; want the cap filled in", req.MaxTokens, err)
	}
}

func TestDailyRequestsAndTheBudgetComeFromTheStore(t *testing.T) {
	ev := &budgetEvents{}
	c := &counter{daily: 10}
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: c, Limiter: ratelimit.NewMemory(), Events: ev})
	tn := quotaTenant(tenant.Quota{DailyRequests: 10, MonthlyBudgetUSD: money.MustParse("10")})
	if err := runQuota(t, mw, tn, &provider.CompletionRequest{}); code(err) != pipeline.CodeQuotaExceeded || pipeline.RetryAfter(err) <= 0 {
		t.Fatalf("at the daily limit = %v", err)
	}
	c.daily = 0
	c.spend = money.MustParse("8")
	for i := 0; i < 3; i++ {
		if err := runQuota(t, mw, tn, &provider.CompletionRequest{}); err != nil {
			t.Fatalf("at 80%% of budget = %v", err)
		}
	}
	c.spend = money.MustParse("10")
	for i := 0; i < 3; i++ {
		if err := runQuota(t, mw, tn, &provider.CompletionRequest{}); code(err) != pipeline.CodeBudgetExceeded {
			t.Fatalf("at the budget = %v", err)
		}
	}
	if ev.warned != 1 || ev.exceeded != 1 {
		t.Fatalf("warned %d exceeded %d; each fires once per tenant per month", ev.warned, ev.exceeded)
	}
}

func TestAStoreFailureRefusesTheMoneyCheck(t *testing.T) {
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{err: errors.New("pg down")}, Limiter: ratelimit.NewMemory()})
	err := runQuota(t, mw, quotaTenant(tenant.Quota{MonthlyBudgetUSD: money.MustParse("1")}), &provider.CompletionRequest{})
	if status, c := pipeline.HTTPStatus(err); status != 503 || c != pipeline.CodeUnavailable {
		t.Fatalf("budget check with the store down = %v", err)
	}
}

func TestRPMAndTPMRefuseWithAWait(t *testing.T) {
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: ratelimit.NewMemory()})
	tn := quotaTenant(tenant.Quota{RPM: 2})
	_ = runQuota(t, mw, tn, &provider.CompletionRequest{})
	_ = runQuota(t, mw, tn, &provider.CompletionRequest{})
	err := runQuota(t, mw, tn, &provider.CompletionRequest{})
	if code(err) != pipeline.CodeRateLimited || pipeline.RetryAfter(err) <= 0 {
		t.Fatalf("third request at 2 RPM = %v", err)
	}
	tp := quotaTenant(tenant.Quota{TPM: 150})
	_ = runQuota(t, mw, tp, &provider.CompletionRequest{}) // charges 100
	_ = runQuota(t, mw, tp, &provider.CompletionRequest{}) // 100 < 150 before: allowed, charges to 200
	if err := runQuota(t, mw, tp, &provider.CompletionRequest{}); code(err) != pipeline.CodeRateLimited {
		t.Fatalf("over TPM = %v", err)
	}
}

func TestALimiterFailureLetsTheRequestThrough(t *testing.T) {
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: brokenLimiter{}, GlobalRPM: 1})
	for i := 0; i < 3; i++ {
		if err := runQuota(t, mw, quotaTenant(tenant.Quota{RPM: 1, TPM: 1}), &provider.CompletionRequest{}); err != nil {
			t.Fatalf("limiter down = %v; want allowed", err)
		}
	}
	if mw.LimiterErrors() == 0 {
		t.Fatal("limiter errors must be counted")
	}
}

func TestTheGlobalLimitAppliesWithoutATenant(t *testing.T) {
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: ratelimit.NewMemory(), GlobalRPM: 1})
	next := func(context.Context) (*pipeline.Response, error) { return &pipeline.Response{}, nil }
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{}, State: map[string]any{}}
	if _, err := mw.Process(context.Background(), req, next); err != nil {
		t.Fatal(err)
	}
	if _, err := mw.Process(context.Background(), req, next); code(err) != pipeline.CodeRateLimited {
		t.Fatalf("second unattributed request at a global 1 RPM = %v", err)
	}
}

func TestAStreamChargesTPMAtCloseFromItsUsageChunk(t *testing.T) {
	lim := ratelimit.NewMemory()
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: lim})
	tn := quotaTenant(tenant.Quota{TPM: 100})
	st := testutil.NewFakeStream([]*provider.StreamChunk{
		{Kind: provider.EventDelta, Delta: provider.Delta{Content: "hi"}},
		{Kind: provider.EventUsage, Usage: &provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}},
	}, nil)
	ctx := middlewares.WithTenantForTest(context.Background(), tn)
	resp, err := mw.Process(ctx, &pipeline.Request{Type: pipeline.RequestStream, Completion: &provider.CompletionRequest{}, State: map[string]any{}},
		func(context.Context) (*pipeline.Response, error) { return &pipeline.Response{Stream: st}, nil })
	if err != nil {
		t.Fatal(err)
	}
	check := func() ratelimit.Decision {
		d, err := lim.Allow(context.Background(), "tpm:"+tn.ID.String(), 0, 100, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	for {
		if _, err := resp.Stream.Next(context.Background()); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			break
		}
	}
	if d := check(); !d.Allowed || d.Count != 0 {
		t.Fatalf("before Close: allowed %v count %d; nothing is charged until the stream closes", d.Allowed, d.Count)
	}
	_ = resp.Stream.Close()
	_ = resp.Stream.Close() // a second close must not charge again
	if d := check(); d.Allowed || d.Count != 120 {
		t.Fatalf("after Close: allowed %v count %d; want refused at 120 tokens", d.Allowed, d.Count)
	}
}

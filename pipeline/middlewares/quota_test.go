package middlewares_test

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
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
	cause := errors.New("pg down")
	cases := map[string]tenant.Quota{
		"budget": {MonthlyBudgetUSD: money.MustParse("1")},
		"daily":  {DailyRequests: 5},
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{err: cause}, Limiter: ratelimit.NewMemory()})
			err := runQuota(t, mw, quotaTenant(q), &provider.CompletionRequest{})
			if status, c := pipeline.HTTPStatus(err); status != 503 || c != pipeline.CodeUnavailable {
				t.Fatalf("%s check with the store down = %v", name, err)
			}
			if !errors.Is(err, cause) {
				t.Fatalf("%s refusal must wrap the store error, got %v", name, err)
			}
		})
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
	if err := runQuota(t, mw, tp, &provider.CompletionRequest{}); err != nil { // charges 100
		t.Fatalf("first request at 150 TPM = %v", err)
	}
	if err := runQuota(t, mw, tp, &provider.CompletionRequest{}); err != nil { // 100 < 150 before: allowed, charges to 200
		t.Fatalf("second request at 150 TPM = %v", err)
	}
	if err := runQuota(t, mw, tp, &provider.CompletionRequest{}); code(err) != pipeline.CodeRateLimited {
		t.Fatalf("over TPM = %v", err)
	}
}

type errorLog struct {
	mu    sync.Mutex
	lines [][]any
}

func (l *errorLog) Error(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, append([]any{msg}, args...))
}

func TestALimiterFailureLetsTheRequestThrough(t *testing.T) {
	log := &errorLog{}
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: brokenLimiter{}, GlobalRPM: 1, Log: log})
	tn := quotaTenant(tenant.Quota{RPM: 1, TPM: 1})
	for i := 0; i < 3; i++ {
		if err := runQuota(t, mw, tn, &provider.CompletionRequest{}); err != nil {
			t.Fatalf("limiter down = %v; want allowed", err)
		}
	}
	// Per request: the global check, the RPM check, the TPM check and the
	// TPM charge (the fake completion reports 100 tokens).
	if got := mw.LimiterErrors(); got != 12 {
		t.Fatalf("limiter errors = %d, want 12", got)
	}
	if len(log.lines) != 12 {
		t.Fatalf("logged %d limiter failures, want 12", len(log.lines))
	}
	first := log.lines[0]
	if first[0] != "nexus: rate limiter failed" {
		t.Fatalf("log message = %v", first[0])
	}
	keys := map[any]bool{}
	for _, l := range log.lines {
		for i := 1; i+1 < len(l); i += 2 {
			if l[i] == "key" {
				keys[l[i+1]] = true
			}
		}
	}
	for _, want := range []string{"global:rpm", "rpm:" + tn.ID.String(), "tpm:" + tn.ID.String()} {
		if !keys[want] {
			t.Errorf("no limiter failure logged for key %q; logged %v", want, keys)
		}
	}
}

func TestNewQuotaPanicsWithoutItsRequiredParts(t *testing.T) {
	for name, cfg := range map[string]middlewares.QuotaConfig{
		"Usage":   {Limiter: ratelimit.NewMemory()},
		"Limiter": {Usage: &counter{}},
	} {
		func() {
			defer func() {
				r := recover()
				msg, _ := r.(string)
				if msg == "" || !strings.Contains(msg, "QuotaConfig."+name) {
					t.Errorf("NewQuota without %s panicked with %v; want a message naming QuotaConfig.%s", name, r, name)
				}
			}()
			middlewares.NewQuota(cfg)
		}()
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

// fixedClock returns a Now that always reads at.
func fixedClock(at time.Time) func() time.Time { return func() time.Time { return at } }

func TestRetryTimesAreExact(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		q    tenant.Quota
		c    *counter
		code string
		want time.Duration
	}{
		{"daily, 30 minutes before UTC midnight", time.Date(2026, 10, 7, 23, 30, 0, 0, time.UTC),
			tenant.Quota{DailyRequests: 1}, &counter{daily: 1}, pipeline.CodeQuotaExceeded, 30 * time.Minute},
		{"budget on 15 Dec waits for 1 Jan of the next year", time.Date(2026, 12, 15, 9, 0, 0, 0, time.UTC),
			tenant.Quota{MonthlyBudgetUSD: money.MustParse("1")}, &counter{spend: money.MustParse("1")}, pipeline.CodeBudgetExceeded,
			time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).Sub(time.Date(2026, 12, 15, 9, 0, 0, 0, time.UTC))},
		{"budget on 15 Oct waits for 1 Nov", time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC),
			tenant.Quota{MonthlyBudgetUSD: money.MustParse("1")}, &counter{spend: money.MustParse("2")}, pipeline.CodeBudgetExceeded,
			time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).Sub(time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: c.c, Limiter: ratelimit.NewMemory(), Now: fixedClock(c.now)})
			err := runQuota(t, mw, quotaTenant(c.q), &provider.CompletionRequest{})
			if code(err) != c.code {
				t.Fatalf("code = %q (%v), want %q", code(err), err, c.code)
			}
			if got := pipeline.RetryAfter(err); got != c.want {
				t.Fatalf("retry after = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNoBudgetMeansTheStoreIsNeverRead(t *testing.T) {
	for _, budget := range []string{"0", "-5"} {
		mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{err: errors.New("pg down")}, Limiter: ratelimit.NewMemory()})
		tn := quotaTenant(tenant.Quota{MonthlyBudgetUSD: money.MustParse(budget)})
		if err := runQuota(t, mw, tn, &provider.CompletionRequest{}); err != nil {
			t.Fatalf("budget %s with a failing store = %v; want no budget, so no read", budget, err)
		}
	}
}

// pctEvents records the percentage each warning carried.
type pctEvents struct {
	budgetEvents
	pcts []float64
}

func (p *pctEvents) EmitBudgetWarning(ctx context.Context, tid id.TenantID, pct float64) {
	p.budgetEvents.EmitBudgetWarning(ctx, tid, pct)
	p.mu.Lock()
	p.pcts = append(p.pcts, pct)
	p.mu.Unlock()
}

func TestBudgetEdges(t *testing.T) {
	cases := []struct {
		name         string
		spend        string
		code         string // "" means the request passes
		warned       int
		exceeded     int
		wantWarnedAt float64
	}{
		{"7.99 of 10 is under 80% and does not warn", "7.99", "", 0, 0, 0},
		{"8 of 10 warns at 80", "8", "", 1, 0, 80},
		{"9.999999 of 10 passes", "9.999999", "", 1, 0, 99.99999},
		{"a jump from under 80% to over 100% reports exceeded only", "12", pipeline.CodeBudgetExceeded, 0, 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := &pctEvents{}
			mw := middlewares.NewQuota(middlewares.QuotaConfig{
				Usage: &counter{spend: money.MustParse(c.spend)}, Limiter: ratelimit.NewMemory(), Events: ev,
			})
			tn := quotaTenant(tenant.Quota{MonthlyBudgetUSD: money.MustParse("10")})
			err := runQuota(t, mw, tn, &provider.CompletionRequest{})
			if (c.code == "" && err != nil) || (c.code != "" && code(err) != c.code) {
				t.Fatalf("err = %v, want code %q", err, c.code)
			}
			if ev.warned != c.warned || ev.exceeded != c.exceeded {
				t.Fatalf("warned %d exceeded %d, want %d and %d", ev.warned, ev.exceeded, c.warned, c.exceeded)
			}
			if c.warned == 1 && math.Abs(ev.pcts[0]-c.wantWarnedAt) > 1e-9 {
				t.Fatalf("warning carried %v percent, want %v", ev.pcts[0], c.wantWarnedAt)
			}
		})
	}
}

// tpmCount reads tpm:<tenant> without charging it.
func tpmCount(t *testing.T, lim ratelimit.Limiter, tn *tenant.Tenant) int64 {
	t.Helper()
	d, err := lim.Allow(context.Background(), "tpm:"+tn.ID.String(), 0, 1000, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return d.Count
}

func TestACacheHitIsNeverChargedToTPM(t *testing.T) {
	usage := provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
	for _, kind := range []string{"completion", "stream"} {
		t.Run(kind, func(t *testing.T) {
			lim := ratelimit.NewMemory()
			mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: lim})
			tn := quotaTenant(tenant.Quota{TPM: 100})
			req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{}, State: map[string]any{}}
			var st *testutil.FakeStream
			resp, err := mw.Process(middlewares.WithTenantForTest(context.Background(), tn), req,
				func(context.Context) (*pipeline.Response, error) {
					req.State[pipeline.StateCacheHit] = true
					if kind == "completion" {
						return &pipeline.Response{Completion: &provider.CompletionResponse{Usage: usage}}, nil
					}
					st = testutil.NewFakeStream([]*provider.StreamChunk{
						{Kind: provider.EventUsage, Usage: &usage},
					}, &usage)
					return &pipeline.Response{Stream: st}, nil
				})
			if err != nil {
				t.Fatal(err)
			}
			if kind == "stream" {
				if _, ok := resp.Stream.(*testutil.FakeStream); !ok {
					t.Fatalf("a cache-hit stream was wrapped as %T; it must be returned as it came", resp.Stream)
				}
				for {
					if _, err := resp.Stream.Next(context.Background()); err != nil {
						break
					}
				}
				_ = resp.Stream.Close()
			}
			if got := tpmCount(t, lim, tn); got != 0 {
				t.Fatalf("tpm count after a cache-hit %s = %d, want 0", kind, got)
			}
		})
	}
}

func TestTheDailyChargeRefusesUntilUTCMidnight(t *testing.T) {
	now := time.Date(2026, 10, 7, 23, 30, 0, 0, time.UTC)
	lim := ratelimit.NewMemory(ratelimit.WithClock(fixedClock(now)))
	// The store has recorded nothing yet, as when requests run together.
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: lim, Now: fixedClock(now)})
	tn := quotaTenant(tenant.Quota{DailyRequests: 2})
	for i := 1; i <= 2; i++ {
		if err := runQuota(t, mw, tn, &provider.CompletionRequest{}); err != nil {
			t.Fatalf("request %d of 2 = %v", i, err)
		}
	}
	err := runQuota(t, mw, tn, &provider.CompletionRequest{})
	if code(err) != pipeline.CodeQuotaExceeded || pipeline.RetryAfter(err) != 30*time.Minute {
		t.Fatalf("third request with the store still at 0 = %v (retry after %v); want quota_exceeded, 30m to UTC midnight", err, pipeline.RetryAfter(err))
	}
}

func TestADailyChargeTheLimiterCannotMakeLetsTheRequestThrough(t *testing.T) {
	log := &errorLog{}
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: brokenLimiter{}, Log: log})
	tn := quotaTenant(tenant.Quota{DailyRequests: 1})
	for i := 0; i < 3; i++ {
		if err := runQuota(t, mw, tn, &provider.CompletionRequest{}); err != nil {
			t.Fatalf("request %d with the limiter down = %v; want allowed", i, err)
		}
	}
	if got := mw.LimiterErrors(); got != 3 {
		t.Fatalf("limiter errors = %d, want 3 (one daily charge per request)", got)
	}
}

func TestARefusalBeforeTheDailyChargeDoesNotUseUpTheDay(t *testing.T) {
	lim := ratelimit.NewMemory()
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: lim})
	tn := quotaTenant(tenant.Quota{DailyRequests: 5, MaxTokensPerReq: 10})
	for i := 0; i < 3; i++ {
		if err := runQuota(t, mw, tn, &provider.CompletionRequest{MaxTokens: 11}); code(err) != pipeline.CodeInvalidRequest {
			t.Fatalf("over the token cap = %v", err)
		}
	}
	d, err := lim.Allow(context.Background(), "daily:"+tn.ID.String(), 0, 5, 24*time.Hour)
	if err != nil || d.Count != 0 {
		t.Fatalf("daily count after three refusals = %d, %v; want 0", d.Count, err)
	}
}

func TestANegativeMaxTokensIsRefused(t *testing.T) {
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: ratelimit.NewMemory()})
	for name, tn := range map[string]*tenant.Tenant{
		"under a cap": quotaTenant(tenant.Quota{MaxTokensPerReq: 500}),
		"no cap":      quotaTenant(tenant.Quota{}),
	} {
		if err := runQuota(t, mw, tn, &provider.CompletionRequest{MaxTokens: -1}); code(err) != pipeline.CodeInvalidRequest {
			t.Fatalf("%s: max_tokens -1 = %v; want invalid_request", name, err)
		}
	}
	next := func(context.Context) (*pipeline.Response, error) { return &pipeline.Response{}, nil }
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{MaxTokens: -5}, State: map[string]any{}}
	if _, err := mw.Process(context.Background(), req, next); code(err) != pipeline.CodeInvalidRequest {
		t.Fatalf("unattributed max_tokens -5 = %v; want invalid_request", err)
	}
}

// hungLimiter never answers until its context ends, like a Redis that
// accepts the connection and goes quiet.
type hungLimiter struct{}

func (hungLimiter) Allow(ctx context.Context, _ string, _, _ int64, _ time.Duration) (ratelimit.Decision, error) {
	<-ctx.Done()
	return ratelimit.Decision{}, ctx.Err()
}
func (hungLimiter) Kind() string { return "redis" }

func TestAHungLimiterFailsOpenWithinItsBound(t *testing.T) {
	mw := middlewares.NewQuota(middlewares.QuotaConfig{Usage: &counter{}, Limiter: hungLimiter{}})
	tn := quotaTenant(tenant.Quota{RPM: 10, DailyRequests: 10})
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- runQuota(t, mw, tn, &provider.CompletionRequest{}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("hung limiter = %v; want the request through", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the request was still waiting on the limiter after 3s")
	}
	// Two pre-request checks (RPM, then the daily charge), 250ms each at most.
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Fatalf("took %v; each check is bounded at 250ms", took)
	}
	if got := mw.LimiterErrors(); got != 2 {
		t.Fatalf("limiter errors = %d, want 2", got)
	}
}

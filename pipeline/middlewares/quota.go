package middlewares

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/ratelimit"
)

// UsageCounter is the store-of-record read the money checks need.
// usage.Service satisfies it.
type UsageCounter interface {
	DailyRequests(ctx context.Context, tenantID string) (int, error)
	MonthlySpend(ctx context.Context, tenantID string) (money.USD, error)
}

// BudgetEvents receives budget events. *plugin.Registry satisfies it.
type BudgetEvents interface {
	EmitBudgetWarning(ctx context.Context, tenantID id.TenantID, usedPct float64)
	EmitBudgetExceeded(ctx context.Context, tenantID id.TenantID)
}

// QuotaConfig configures the quota stage. Usage and Limiter are required.
// The TPM charge after a request is synchronous, with a 2 second timeout, so
// a slow limiter delays the end of a completion or the Close of a stream by
// up to that long.
type QuotaConfig struct {
	Usage     UsageCounter      // required
	Limiter   ratelimit.Limiter // required
	Events    BudgetEvents
	GlobalRPM int
	Log       UsageLogger
	Now       func() time.Time
}

// QuotaMiddleware enforces the tenant's per-request token cap, its daily
// requests and monthly budget (read from the store of record, failing
// closed), and its RPM and TPM (through the limiter, failing open). It
// also enforces the gateway-wide GlobalRPM for every request.
//
// The daily cap is hard: besides the store count, each request is charged
// to a UTC-day window in the limiter, so requests in flight at the same time
// cannot all slip under it. It holds per replica with the memory limiter and
// across replicas with Redis. The budget is soft and has no such charge: a
// request's cost is known only once it ends, so every request admitted while
// the stored spend is under the budget is served. The overshoot can reach
// every request in flight times its cost. A stream counts at Close, an
// unpriced model counts nothing, and neither does a record whose insert
// failed. Pair a budget with RPM and MaxTokensPerReq to bound it.
//
// The checks run in order and a refused request is not refunded: one
// refused by a later check has already been charged against GlobalRPM and
// RPM if those ran before it.
type QuotaMiddleware struct {
	cfg           QuotaConfig
	limiterErrors atomic.Int64
	mu            sync.Mutex
	warned        map[string]string // tenant -> month already warned
	exceeded      map[string]string // tenant -> month already reported
}

// NewQuota returns the quota stage.
func NewQuota(cfg QuotaConfig) *QuotaMiddleware {
	if cfg.Usage == nil {
		panic("middlewares: NewQuota needs QuotaConfig.Usage")
	}
	if cfg.Limiter == nil {
		panic("middlewares: NewQuota needs QuotaConfig.Limiter")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &QuotaMiddleware{cfg: cfg, warned: map[string]string{}, exceeded: map[string]string{}}
}

func (*QuotaMiddleware) Name() string  { return "quota" }
func (*QuotaMiddleware) Priority() int { return 50 }

// LimiterErrors counts limiter failures that let a request through.
func (m *QuotaMiddleware) LimiterErrors() int64 { return m.limiterErrors.Load() }

func (m *QuotaMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.cfg.GlobalRPM > 0 {
		if r := m.allow(ctx, "global:rpm", 1, int64(m.cfg.GlobalRPM), "requests a minute across the gateway"); r != nil {
			return nil, r
		}
	}
	t, ok := TenantFromContext(ctx)
	if !ok {
		return next(ctx)
	}
	tid, q := t.ID.String(), t.Quota

	if q.MaxTokensPerReq > 0 && req.Completion != nil {
		switch {
		case req.Completion.MaxTokens > q.MaxTokensPerReq:
			return nil, &pipeline.RefusalError{Code: pipeline.CodeInvalidRequest, Status: 400,
				Message: "max_tokens is above the tenant's cap of " + strconv.Itoa(q.MaxTokensPerReq), Limit: strconv.Itoa(q.MaxTokensPerReq)}
		case req.Completion.MaxTokens == 0:
			req.Completion.MaxTokens = q.MaxTokensPerReq
		}
	}

	now := m.cfg.Now().UTC()
	if q.DailyRequests > 0 {
		n, err := m.cfg.Usage.DailyRequests(ctx, tid)
		if err != nil {
			return nil, unavailable("daily request count", err)
		}
		if n >= q.DailyRequests {
			tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
			return nil, &pipeline.RefusalError{Code: pipeline.CodeQuotaExceeded, Status: 429,
				Message: "daily request quota reached", Limit: strconv.Itoa(q.DailyRequests), RetryAfter: tomorrow.Sub(now)}
		}
	}

	if q.MonthlyBudgetUSD.IsPositive() {
		spend, err := m.cfg.Usage.MonthlySpend(ctx, tid)
		if err != nil {
			return nil, unavailable("monthly spend", err)
		}
		month := now.Format("2006-01")
		// Soft limit: cost is only known after a request ends, so every
		// request admitted before the spend is stored completes. See the
		// type's doc for the bound.
		if spend.Cmp(q.MonthlyBudgetUSD) >= 0 {
			if m.once(m.exceeded, tid, month) && m.cfg.Events != nil {
				m.cfg.Events.EmitBudgetExceeded(ctx, t.ID)
			}
			nextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
			return nil, &pipeline.RefusalError{Code: pipeline.CodeBudgetExceeded, Status: 429,
				Message: "monthly budget reached", Limit: q.MonthlyBudgetUSD.String(), RetryAfter: nextMonth.Sub(now)}
		}
		// 80%: spend × 5 ≥ budget × 4, in exact decimals.
		if spend.Mul(5).Cmp(q.MonthlyBudgetUSD.Mul(4)) >= 0 && m.once(m.warned, tid, month) && m.cfg.Events != nil {
			m.cfg.Events.EmitBudgetWarning(ctx, t.ID, spend.Ratio(q.MonthlyBudgetUSD)*100)
		}
	}

	if q.RPM > 0 {
		if r := m.allow(ctx, "rpm:"+tid, 1, int64(q.RPM), "requests a minute"); r != nil {
			return nil, r
		}
	}
	tpmKey := "tpm:" + tid
	if q.TPM > 0 {
		if r := m.allow(ctx, tpmKey, 0, int64(q.TPM), "tokens a minute"); r != nil {
			return nil, r
		}
	}
	// The daily cap is charged last, once every other check has passed, so a
	// request refused for any other reason never uses up a day's allowance.
	// The store count above covers what the limiter cannot see (a restart of
	// the memory limiter); the charge here holds the cap when requests run
	// at the same time, because the store only counts a request once it has
	// been recorded.
	if q.DailyRequests > 0 {
		if r := m.chargeDay(ctx, tid, int64(q.DailyRequests)); r != nil {
			return nil, r
		}
	}

	resp, err := next(ctx)
	// Only a request that succeeded is charged to TPM. A failed one is not:
	// an output guard refusal, or the tokens spent on attempts the retry
	// stage threw away, never reach this point. RPM still bounds them.
	if q.TPM > 0 && err == nil && resp != nil {
		switch {
		case stateBool(req, pipeline.StateCacheHit):
			// A cache hit called no provider, streamed or not: it uses no
			// tokens a minute. This case comes first so a replayed stream is
			// never wrapped, and its Usage() is never charged.
		case resp.Stream != nil:
			resp.Stream = &tpmStream{inner: resp.Stream, charge: func(n int) { m.charge(tpmKey, n, int64(q.TPM)) }}
		case resp.Completion != nil:
			m.charge(tpmKey, resp.Completion.Usage.TotalTokens, int64(q.TPM))
		case resp.Embedding != nil:
			m.charge(tpmKey, resp.Embedding.Usage.TotalTokens, int64(q.TPM))
		}
	}
	return resp, err
}

func unavailable(what string, err error) *pipeline.RefusalError {
	return &pipeline.RefusalError{Code: pipeline.CodeUnavailable, Status: 503, Message: what + " is unavailable", Cause: err}
}

// once reports whether tenant has not been seen in seen for month, and
// records it.
func (m *QuotaMiddleware) once(seen map[string]string, tenant, month string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if seen[tenant] == month {
		return false
	}
	seen[tenant] = month
	return true
}

// try charges n against key's window. It reports false when the limiter
// failed: the failure is counted and logged, and the caller lets the request
// through.
func (m *QuotaMiddleware) try(ctx context.Context, key string, n, limit int64, window time.Duration) (ratelimit.Decision, bool) {
	d, err := m.cfg.Limiter.Allow(ctx, key, n, limit, window)
	if err != nil {
		m.limiterErrors.Add(1)
		if m.cfg.Log != nil {
			m.cfg.Log.Error("nexus: rate limiter failed", "limiter", m.cfg.Limiter.Kind(), "key", key, "error", err)
		}
		return ratelimit.Decision{}, false
	}
	return d, true
}

// allow charges n and refuses when the window was already at limit. A
// limiter failure lets the request through and is counted.
func (m *QuotaMiddleware) allow(ctx context.Context, key string, n, limit int64, what string) *pipeline.RefusalError {
	d, ok := m.try(ctx, key, n, limit, time.Minute)
	if !ok || d.Allowed {
		return nil
	}
	l := strconv.FormatInt(limit, 10)
	return &pipeline.RefusalError{Code: pipeline.CodeRateLimited, Status: 429, Message: l + " " + what, Limit: l, RetryAfter: d.RetryAfter}
}

// chargeDay charges one request against the tenant's UTC day. A 24 hour
// window truncates to UTC midnight in both limiters, so the window is the
// calendar day and RetryAfter runs to the next midnight. A limiter failure
// lets the request through and is counted, as RPM does.
func (m *QuotaMiddleware) chargeDay(ctx context.Context, tid string, limit int64) *pipeline.RefusalError {
	d, ok := m.try(ctx, "daily:"+tid, 1, limit, 24*time.Hour)
	if !ok || d.Allowed {
		return nil
	}
	return &pipeline.RefusalError{Code: pipeline.CodeQuotaExceeded, Status: 429,
		Message: "daily request quota reached", Limit: strconv.FormatInt(limit, 10), RetryAfter: d.RetryAfter}
}

// charge records tokens a request used. It runs after the request, on a
// fresh context: the request's may already be done. The request is over,
// so there is nothing to refuse; the next one sees the new count.
func (m *QuotaMiddleware) charge(key string, tokens int, limit int64) {
	if tokens <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	m.try(ctx, key, int64(tokens), limit, time.Minute)
}

// tpmStream charges the tokens a stream reported, once, when it is closed.
type tpmStream struct {
	inner  provider.Stream
	charge func(int)
	mu     sync.Mutex
	tokens int
	once   sync.Once
}

func (s *tpmStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	chunk, err := s.inner.Next(ctx)
	if chunk != nil && chunk.Usage != nil {
		s.mu.Lock()
		s.tokens = chunk.Usage.TotalTokens
		s.mu.Unlock()
	}
	return chunk, err
}

func (s *tpmStream) Close() error {
	err := s.inner.Close()
	s.once.Do(func() {
		s.mu.Lock()
		n := s.tokens
		s.mu.Unlock()
		if n == 0 {
			if u := s.inner.Usage(); u != nil {
				n = u.TotalTokens
			}
		}
		s.charge(n)
	})
	return err
}

func (s *tpmStream) Usage() *provider.Usage { return s.inner.Usage() }

package middlewares

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/usage"
)

// PriceLookup finds a model's list price at the provider that served it.
// *model.PriceBook implements it.
type PriceLookup interface {
	Price(ctx context.Context, providerName string, modelIDs ...string) (provider.Pricing, bool)
}

// UsageLogger receives insert failures. nexus.Logger implements it.
type UsageLogger interface {
	Error(msg string, args ...any)
}

// UsageMiddleware records exactly one usage record per request. It sits at
// priority 15, outside everything but the request id and tracing, so it
// sees cache hits, guard blocks, refusals, the final result after retries
// and the request's total latency. It prices the request at the provider
// that served it and says what happened and whether the cost is known.
//
// Records are stored asynchronously. A failed insert is logged and counted
// (InsertErrors), and Flush waits for the inserts still in flight.
type UsageMiddleware struct {
	usage  usage.Service
	prices PriceLookup
	log    UsageLogger

	inflight     sync.WaitGroup
	insertErrors atomic.Int64
}

// NewUsage creates the usage middleware. prices and log may be nil: without
// prices every request is unpriced; without log failures are only counted.
func NewUsage(u usage.Service, prices PriceLookup, log UsageLogger) *UsageMiddleware {
	return &UsageMiddleware{usage: u, prices: prices, log: log}
}

func (m *UsageMiddleware) Name() string  { return "usage" }
func (m *UsageMiddleware) Priority() int { return 15 }

// InsertErrors is how many records failed to store since start.
func (m *UsageMiddleware) InsertErrors() int64 { return m.insertErrors.Load() }

// Flush waits for in-flight inserts, or until ctx is done.
func (m *UsageMiddleware) Flush(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		m.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *UsageMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.usage == nil {
		return next(ctx)
	}
	start := time.Now()
	resp, err := next(ctx)

	rec := m.newRecord(ctx, req, start)
	if err == nil && resp != nil && resp.Stream != nil {
		resp.Stream = &usageRecordingStream{inner: resp.Stream, mw: m, ctx: ctx, rec: rec, req: req, start: start}
		return resp, nil
	}
	rec.Latency = time.Since(start)
	m.classify(ctx, rec, req, resp, err)
	m.record(rec)
	return resp, err
}

func (m *UsageMiddleware) newRecord(ctx context.Context, req *pipeline.Request, start time.Time) *usage.Record {
	rec := &usage.Record{ID: id.NewUsageID(), CreatedAt: start.UTC()}
	if rid, err := id.ParseRequestID(pipeline.RequestID(ctx)); err == nil {
		rec.RequestID = rid
	}
	tenant, key := requestIdentity(req)
	if tid, err := id.ParseTenantID(tenant); err == nil {
		rec.TenantID = tid
	}
	if kid, err := id.ParseKeyID(key); err == nil {
		rec.KeyID = kid
	}
	return rec
}

// classify fills a non-stream record from the request's outcome.
func (m *UsageMiddleware) classify(ctx context.Context, rec *usage.Record, req *pipeline.Request, resp *pipeline.Response, err error) {
	rec.Provider = stateString(req, pipeline.StateProviderName)
	rec.Model = requestModel(req)
	cacheHit := stateBool(req, pipeline.StateCacheHit)

	var blocked *guard.BlockedError
	var refused pipeline.Refusal
	switch {
	case errors.As(err, &blocked):
		rec.Outcome, rec.BlockedBy, rec.StatusCode = usage.OutcomeBlocked, blocked.Guard, 400
		// A block is classified by its phase, not by whether it carries usage.
		// An input block happens before any provider is called: not charged.
		// An output block happens after the provider answered, so it was
		// charged: at the tokens the blocked response consumed when we know
		// them, at an unknown cost when we do not (a stream guard blocks
		// mid-stream, without the response's usage).
		switch {
		case blocked.Phase == guard.PhaseInput:
			notCharged(rec)
		case blocked.Usage != nil:
			if blocked.Provider != "" {
				rec.Provider = blocked.Provider
			}
			setTokens(rec, *blocked.Usage)
			m.price(ctx, rec, *blocked.Usage, false, blocked.Model)
		default:
			rec.PricingStatus = usage.PricingUnknown
		}
	case errors.As(err, &refused):
		rec.Outcome, rec.RefusalCode, rec.StatusCode = usage.OutcomeRefused, refused.RefusalCode(), refused.StatusCode()
		notCharged(rec)
	case err != nil:
		rec.Outcome, rec.StatusCode = usage.OutcomeError, 500
		if rec.Provider == "" {
			notCharged(rec)
		} else {
			rec.PricingStatus = usage.PricingUnknown
		}
	case resp != nil && resp.Embedding != nil:
		rec.Outcome, rec.StatusCode = usage.OutcomeOK, 200
		setTokens(rec, resp.Embedding.Usage)
		m.price(ctx, rec, resp.Embedding.Usage, true, resp.Embedding.Model)
	case resp != nil && resp.Completion != nil && (cacheHit || resp.Completion.Cached):
		rec.Outcome, rec.StatusCode = usage.OutcomeCached, 200
		setTokens(rec, resp.Completion.Usage)
		cached(rec)
	case resp != nil && resp.Completion != nil:
		rec.Outcome, rec.StatusCode = usage.OutcomeOK, 200
		setTokens(rec, resp.Completion.Usage)
		m.price(ctx, rec, resp.Completion.Usage, false, resp.Completion.Model)
	default:
		rec.Outcome, rec.StatusCode = usage.OutcomeOK, 200
		rec.PricingStatus = usage.PricingUnpricedModel
	}
}

// price sets the cost from the list price at the provider that served the
// request: the requested model first (aliases already resolved), then the
// model the provider reported.
func (m *UsageMiddleware) price(ctx context.Context, rec *usage.Record, u provider.Usage, embedding bool, respModel string) {
	if m.prices == nil {
		rec.CostUSD, rec.PricingStatus = nil, usage.PricingUnpricedModel
		return
	}
	p, ok := m.prices.Price(ctx, rec.Provider, rec.Model, respModel)
	if !ok {
		rec.CostUSD, rec.PricingStatus = nil, usage.PricingUnpricedModel
		return
	}
	rec.CostUSD, rec.PricingStatus = model.Cost(u, p, embedding)
}

func (m *UsageMiddleware) record(rec *usage.Record) {
	m.inflight.Add(1)
	go func() {
		defer m.inflight.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.usage.Record(ctx, rec); err != nil {
			m.insertErrors.Add(1)
			if m.log != nil {
				m.log.Error("nexus: storing a usage record failed", "request_id", rec.RequestID.String(), "error", err)
			}
		}
	}()
}

func stateString(req *pipeline.Request, key string) string {
	if v, ok := req.State[key].(string); ok {
		return v
	}
	return ""
}

func stateBool(req *pipeline.Request, key string) bool {
	if v, ok := req.State[key].(bool); ok {
		return v
	}
	return false
}

func requestModel(req *pipeline.Request) string {
	switch {
	case req.Completion != nil:
		return req.Completion.Model
	case req.Embedding != nil:
		return req.Embedding.Model
	}
	return ""
}

func setTokens(rec *usage.Record, u provider.Usage) {
	rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens = u.PromptTokens, u.CompletionTokens, u.TotalTokens
	if rec.TotalTokens == 0 {
		rec.TotalTokens = rec.PromptTokens + rec.CompletionTokens
	}
}

func notCharged(rec *usage.Record) {
	zero := money.Zero
	rec.CostUSD, rec.PricingStatus = &zero, usage.PricingNotCharged
}

func cached(rec *usage.Record) {
	zero := money.Zero
	rec.Cached, rec.CostUSD, rec.PricingStatus = true, &zero, usage.PricingCached
}

// usageRecordingStream records the stream's usage once, when it is closed.
// It captures token counts from usage chunks as they pass, falls back to the
// merged final response the stream lifecycle stage publishes and to the
// stream's own Usage, and remembers the first error that was not io.EOF so
// a broken stream is recorded as an error.
type usageRecordingStream struct {
	inner provider.Stream
	mw    *UsageMiddleware
	ctx   context.Context
	rec   *usage.Record
	req   *pipeline.Request
	start time.Time

	mu       sync.Mutex
	usage    provider.Usage
	failed   bool
	blocked  *guard.BlockedError
	recorded bool
}

func (s *usageRecordingStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	chunk, err := s.inner.Next(ctx)
	s.mu.Lock()
	if chunk != nil && chunk.Usage != nil {
		s.usage = *chunk.Usage
	}
	if err != nil && !errors.Is(err, io.EOF) {
		s.failed = true
		var blocked *guard.BlockedError
		if s.blocked == nil && errors.As(err, &blocked) {
			s.blocked = blocked
		}
	}
	s.mu.Unlock()
	return chunk, err
}

func (s *usageRecordingStream) Close() error {
	s.mu.Lock()
	already := s.recorded
	s.recorded = true
	s.mu.Unlock()

	closeErr := s.inner.Close()
	if already {
		return closeErr
	}

	s.mu.Lock()
	u, failed, blocked := s.usage, s.failed, s.blocked
	s.mu.Unlock()
	respModel := ""
	if v, ok := s.req.State[StateKeyStreamFinalResponse].(*provider.CompletionResponse); ok && v != nil {
		if u.TotalTokens == 0 && u.PromptTokens == 0 && u.CompletionTokens == 0 {
			u = v.Usage
		}
		respModel = v.Model
	}
	if u.TotalTokens == 0 && u.PromptTokens == 0 && u.CompletionTokens == 0 {
		if iu := s.inner.Usage(); iu != nil {
			u = *iu
		}
	}

	rec := s.rec
	rec.Latency = time.Since(s.start)
	rec.Provider = stateString(s.req, pipeline.StateProviderName)
	rec.Model = requestModel(s.req)
	setTokens(rec, u)
	cacheHit := stateBool(s.req, pipeline.StateCacheHit)
	hasTokens := rec.TotalTokens > 0
	switch {
	case blocked != nil && cacheHit:
		// A stream guard stopped a cache replay: nothing was charged.
		rec.Outcome, rec.BlockedBy, rec.StatusCode = usage.OutcomeBlocked, blocked.Guard, 400
		cached(rec)
	case blocked != nil && !hasTokens:
		// The provider already generated output, but no token count reached us.
		rec.Outcome, rec.BlockedBy, rec.StatusCode, rec.PricingStatus = usage.OutcomeBlocked, blocked.Guard, 400, usage.PricingUnknown
	case blocked != nil:
		rec.Outcome, rec.BlockedBy, rec.StatusCode = usage.OutcomeBlocked, blocked.Guard, 400
		s.mw.price(s.ctx, rec, u, false, respModel)
	case cacheHit:
		rec.Outcome, rec.StatusCode = usage.OutcomeCached, 200
		cached(rec)
	case failed && !hasTokens:
		rec.Outcome, rec.StatusCode, rec.PricingStatus = usage.OutcomeError, 500, usage.PricingUnknown
	case failed:
		rec.Outcome, rec.StatusCode = usage.OutcomeError, 500
		s.mw.price(s.ctx, rec, u, false, respModel)
	default:
		rec.Outcome, rec.StatusCode = usage.OutcomeOK, 200
		s.mw.price(s.ctx, rec, u, false, respModel)
	}
	s.mw.record(rec)
	return closeErr
}

func (s *usageRecordingStream) Usage() *provider.Usage { return s.inner.Usage() }

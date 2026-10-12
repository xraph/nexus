package middlewares

import (
	"context"
	"errors"
	"fmt"
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
// (InsertErrors), and Flush waits for the inserts still in flight and for
// the streams still open. After Close, a record that has not started is not
// stored: it is counted and logged as dropped.
type UsageMiddleware struct {
	usage  usage.Service
	prices PriceLookup
	log    UsageLogger

	mu           sync.Mutex
	idle         *sync.Cond // signalled when pending reaches zero
	pending      int        // open streams and inserts in flight, guarded by mu
	closed       bool       // set by Close, guarded by mu
	insertErrors atomic.Int64
}

// NewUsage creates the usage middleware. prices and log may be nil: without
// prices every request is unpriced; without log failures are only counted.
func NewUsage(u usage.Service, prices PriceLookup, log UsageLogger) *UsageMiddleware {
	m := &UsageMiddleware{usage: u, prices: prices, log: log}
	m.idle = sync.NewCond(&m.mu)
	return m
}

func (m *UsageMiddleware) Name() string  { return "usage" }
func (m *UsageMiddleware) Priority() int { return 15 }

// InsertErrors is how many records failed to store since start, including
// records dropped because they arrived after Close.
func (m *UsageMiddleware) InsertErrors() int64 { return m.insertErrors.Load() }

// Pending is how many records are not stored yet: streams still open and
// inserts still in flight.
func (m *UsageMiddleware) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pending
}

// Close stops the stage taking new records. A stream that was open before
// Close still records when it is closed, so Flush can wait for it; any
// other record after Close is dropped and counted in InsertErrors. Call it
// before Flush when shutting down, so nothing races the store's Close.
func (m *UsageMiddleware) Close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
}

// Flush waits for open streams to be closed and their records and every
// other in-flight insert to be stored, or until ctx is done.
func (m *UsageMiddleware) Flush(ctx context.Context) error {
	m.mu.Lock()
	idle := m.pending == 0
	m.mu.Unlock()
	if idle {
		return nil
	}
	done := make(chan struct{})
	go func() {
		m.mu.Lock()
		for m.pending > 0 {
			m.idle.Wait()
		}
		m.mu.Unlock()
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
	// Reserve before the call, so a shutdown that starts while this request
	// is with the provider waits for its record. A request that arrives
	// after the stage closed is still served; its record is dropped and
	// counted.
	reserved := m.reserve()
	defer func() {
		if r := recover(); r != nil {
			if reserved {
				m.release()
			}
			panic(r)
		}
	}()
	resp, err := next(ctx)

	rec := m.newRecord(ctx, req, start)
	if err == nil && resp != nil && resp.Stream != nil {
		// The reservation moves to the stream and is released after its
		// record is stored, so a shutdown waits for a stream that is open.
		resp.Stream = &usageRecordingStream{inner: resp.Stream, mw: m, ctx: ctx, rec: rec, req: req, start: start, reserved: reserved}
		return resp, nil
	}
	rec.Latency = time.Since(start)
	m.classify(ctx, rec, req, resp, err)
	m.finish(rec, reserved)
	return resp, err
}

func (m *UsageMiddleware) newRecord(ctx context.Context, req *pipeline.Request, start time.Time) *usage.Record {
	rec := &usage.Record{ID: id.NewUsageID(), CreatedAt: start.UTC()}
	if rid, err := id.ParseRequestID(pipeline.RequestID(ctx)); err == nil {
		rec.RequestID = rid
	}
	// The identity this stage was given in its context comes from the
	// authenticating edge, so it wins over the request fields. Either may be
	// set alone; the identity stage refuses a request where they disagree.
	tenant, key := requestIdentity(req)
	if t := pipeline.TenantID(ctx); t != "" {
		tenant = t
	}
	if k := pipeline.KeyID(ctx); k != "" {
		key = k
	}
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
		// An output guard that blocks a cache hit blocked a replay: no
		// provider was called, so it is cached at $0 whatever usage the
		// replayed response carries. Any other output block happens after
		// the provider answered, so it was charged: at the tokens the blocked
		// response consumed when we know them, at an unknown cost when we do
		// not (a stream guard blocks mid-stream, without the response's usage).
		switch {
		case blocked.Phase == guard.PhaseInput:
			notCharged(rec)
		case cacheHit:
			if blocked.Usage != nil {
				setTokens(rec, *blocked.Usage)
			}
			cached(rec)
		case blocked.Usage != nil:
			// The registry name from State is what the price book is keyed
			// by. The response's own provider field is only a fallback.
			if rec.Provider == "" {
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
		// Some refusals are charged to no one: identities that disagree, a
		// tenant that does not exist.
		var re *pipeline.RefusalError
		if errors.As(err, &re) && re.Unattributed {
			rec.TenantID, rec.KeyID = id.TenantID{}, id.KeyID{}
		}
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
	// A cache hit is known only from this request's state. The response's
	// Cached flag lives on an object a cache may share between requests.
	case resp != nil && resp.Completion != nil && cacheHit:
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
	// A request whose priced token kinds are all zero was served, but what it
	// consumed is unknown: a client that left before the usage chunk, a
	// stream that ended without one, a provider that reports only a total
	// for a completion. That is not $0. A free model costs $0 whatever it
	// consumed.
	if !p.Free && !hasPricedTokens(u, embedding) {
		rec.CostUSD, rec.PricingStatus = nil, usage.PricingUnknown
		return
	}
	rec.CostUSD, rec.PricingStatus = model.Cost(u, p, embedding)
}

// hasPricedTokens reports whether u counts any of the tokens a price applies
// to. An embedding is priced from its prompt tokens, or its total when that
// is all the provider reported. A completion is priced from its prompt and
// completion tokens; a total alone cannot be split between the two prices.
func hasPricedTokens(u provider.Usage, embedding bool) bool {
	if embedding {
		return u.PromptTokens > 0 || u.TotalTokens > 0
	}
	return u.PromptTokens > 0 || u.CompletionTokens > 0
}

// reserve counts a record that will be stored later, so Flush waits for it.
// It reports false, and counts nothing, once the stage is closed.
func (m *UsageMiddleware) reserve() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	m.pending++
	return true
}

// release ends a reservation.
func (m *UsageMiddleware) release() {
	m.mu.Lock()
	m.pending--
	if m.pending == 0 {
		m.idle.Broadcast()
	}
	m.mu.Unlock()
}

// finish stores rec on the reservation taken for it, or drops and counts it
// when the stage was already closed.
func (m *UsageMiddleware) finish(rec *usage.Record, reserved bool) {
	if reserved {
		m.insert(rec)
		return
	}
	m.drop(rec)
}

// drop counts and logs a record that arrived after Close.
func (m *UsageMiddleware) drop(rec *usage.Record) {
	m.insertErrors.Add(1)
	if m.log != nil {
		m.log.Error("nexus: usage stage is closed, record dropped", "request_id", rec.RequestID.String())
	}
}

// insert stores a reserved record in the background and ends the
// reservation. A panic from the usage service or its store is counted as a
// failed insert; it must not take the process down from a goroutine.
func (m *UsageMiddleware) insert(rec *usage.Record) {
	go func() {
		defer m.release()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.store(ctx, rec); err != nil {
			m.insertErrors.Add(1)
			if m.log != nil {
				m.log.Error("nexus: storing a usage record failed", "request_id", rec.RequestID.String(), "error", err)
			}
		}
	}()
}

func (m *UsageMiddleware) store(ctx context.Context, rec *usage.Record) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("usage service panicked: %v", r)
		}
	}()
	return m.usage.Record(ctx, rec)
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

	// reserved is true when the stream was counted as pending when it was
	// opened. A stream opened after Close was not, and its record is dropped.
	reserved bool

	mu       sync.Mutex
	usage    provider.Usage
	failed   bool
	quotaCut bool // the stream was cut by its tenant's duration or token limit
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
		if IsQuotaExceeded(err) {
			s.quotaCut = true
		}
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
	u, failed, quotaCut, blocked := s.usage, s.failed, s.quotaCut, s.blocked
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
		streamCutCode(rec, quotaCut)
	case failed:
		rec.Outcome, rec.StatusCode = usage.OutcomeError, 500
		streamCutCode(rec, quotaCut)
		s.mw.price(s.ctx, rec, u, false, respModel)
	default:
		rec.Outcome, rec.StatusCode = usage.OutcomeOK, 200
		s.mw.price(s.ctx, rec, u, false, respModel)
	}
	s.mw.finish(rec, s.reserved)
	return closeErr
}

// streamCutCode marks a stream cut by its tenant's limit as 429
// quota_exceeded. It stays an error, not a refusal: the stream was served in
// part, so its tokens are priced.
func streamCutCode(rec *usage.Record, quotaCut bool) {
	if quotaCut {
		rec.StatusCode, rec.RefusalCode = 429, pipeline.CodeQuotaExceeded
	}
}

func (s *usageRecordingStream) Usage() *provider.Usage { return s.inner.Usage() }

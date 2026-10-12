package middlewares_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/plugin"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
	"github.com/xraph/nexus/usage"
)

type recordingUsage struct {
	mu      sync.Mutex
	records []*usage.Record
	done    chan struct{}
	fail    error
}

func newRecordingUsage() *recordingUsage {
	return &recordingUsage{done: make(chan struct{}, 16)}
}

func (r *recordingUsage) Record(_ context.Context, rec *usage.Record) error {
	r.mu.Lock()
	r.records = append(r.records, rec)
	r.mu.Unlock()
	r.done <- struct{}{}
	return r.fail
}

func (r *recordingUsage) only(t *testing.T) *usage.Record {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.records) != 1 {
		t.Fatalf("got %d records, want exactly 1", len(r.records))
	}
	return r.records[0]
}
func (r *recordingUsage) MonthlySpend(_ context.Context, _ string) (money.USD, error) {
	return money.Zero, nil
}
func (r *recordingUsage) DailyRequests(_ context.Context, _ string) (int, error) { return 0, nil }
func (r *recordingUsage) Summary(_ context.Context, _, _ string) (*usage.Summary, error) {
	return nil, nil //nolint:nilnil // unused
}
func (r *recordingUsage) Series(_ context.Context, _ *usage.SeriesOptions) ([]usage.SeriesPoint, error) {
	return nil, nil
}
func (r *recordingUsage) Query(_ context.Context, _ *usage.QueryOptions) (*usage.QueryResult, error) {
	return &usage.QueryResult{}, nil
}

func TestUsageMiddleware_RecordsForStreams(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{Provider: "openai", Model: "gpt-4o", Delta: provider.Delta{Content: "hi"}},
		{Delta: provider.Delta{Content: " there"}, FinishReason: "stop"},
		{Kind: provider.EventUsage, Usage: &provider.Usage{PromptTokens: 12, CompletionTokens: 4, TotalTokens: 16}},
	}
	stream := testutil.NewFakeStream(chunks, nil)

	rec := newRecordingUsage()
	mw := middlewares.NewUsage(rec, gpt4o, nil)

	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "gpt-4o"},
		Type:       pipeline.RequestStream,
		State:      map[string]any{pipeline.StateProviderName: "openai"},
	}
	resp, err := mw.Process(context.Background(), req, func(_ context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Stream: stream}, nil
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if resp.Stream == nil {
		t.Fatal("usage middleware lost the stream")
	}

	for {
		_, e := resp.Stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("stream error: %v", e)
		}
	}
	_ = resp.Stream.Close()

	select {
	case <-rec.done:
	case <-time.After(2 * time.Second):
		t.Fatal("usage record never written")
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.records) != 1 {
		t.Fatalf("got %d records, want 1", len(rec.records))
	}
	r := rec.records[0]
	if r.PromptTokens != 12 || r.CompletionTokens != 4 || r.TotalTokens != 16 {
		t.Fatalf("token counts: %+v", r)
	}
	if r.Model != "gpt-4o" {
		t.Fatalf("model: %q", r.Model)
	}
	if r.StatusCode != 200 {
		t.Fatalf("status: %d", r.StatusCode)
	}
	// 12 * 2.50/1e6 + 4 * 10/1e6
	if r.Outcome != usage.OutcomeOK || r.PricingStatus != usage.PricingPriced || r.CostUSD == nil || r.CostUSD.String() != "0.00007" {
		t.Fatalf("pricing: %s/%s cost %v", r.Outcome, r.PricingStatus, r.CostUSD)
	}
}

// erroringStream delivers its chunks, then fails with err instead of io.EOF.
type erroringStream struct {
	chunks []*provider.StreamChunk
	err    error
	idx    int
}

func (s *erroringStream) Next(context.Context) (*provider.StreamChunk, error) {
	if s.idx < len(s.chunks) {
		c := s.chunks[s.idx]
		s.idx++
		return c, nil
	}
	return nil, s.err
}
func (s *erroringStream) Close() error           { return nil }
func (s *erroringStream) Usage() *provider.Usage { return nil }

// drainStream runs one stream request through the usage stage, reads the
// stream until it ends or fails, closes it, and returns the one record.
func drainStream(t *testing.T, st provider.Stream, cacheHit bool) *usage.Record {
	t.Helper()
	rec := newRecordingUsage()
	mw := middlewares.NewUsage(rec, gpt4o, nil)
	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "gpt-4o"},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
	}
	resp, err := mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		req.State[pipeline.StateProviderName] = "openai"
		if cacheHit {
			req.State[pipeline.StateCacheHit] = true
		}
		return &pipeline.Response{Stream: st}, nil
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	for {
		if _, e := resp.Stream.Next(context.Background()); e != nil {
			break
		}
	}
	_ = resp.Stream.Close()
	_ = resp.Stream.Close() // a second close must not record again
	if err := mw.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return rec.only(t)
}

func usageChunk(p, c int) *provider.StreamChunk {
	return &provider.StreamChunk{Kind: provider.EventUsage, Usage: &provider.Usage{PromptTokens: p, CompletionTokens: c, TotalTokens: p + c}}
}

func wantRecord(t *testing.T, got *usage.Record, outcome usage.Outcome, status usage.PricingStatus, cost string) {
	t.Helper()
	if got.Outcome != outcome || got.PricingStatus != status {
		t.Fatalf("outcome %s/%s, want %s/%s", got.Outcome, got.PricingStatus, outcome, status)
	}
	switch {
	case cost == "" && got.CostUSD != nil:
		t.Fatalf("cost = %s, want nil", got.CostUSD)
	case cost != "" && (got.CostUSD == nil || got.CostUSD.String() != cost):
		t.Fatalf("cost = %v, want %s", got.CostUSD, cost)
	}
}

func TestUsageRecordsAFailedStream(t *testing.T) {
	t.Parallel()
	boom := errors.New("connection reset")

	t.Run("after reporting tokens", func(t *testing.T) {
		t.Parallel()
		got := drainStream(t, &erroringStream{chunks: []*provider.StreamChunk{usageChunk(100, 20)}, err: boom}, false)
		// 100 * 2.50/1e6 + 20 * 10/1e6
		wantRecord(t, got, usage.OutcomeError, usage.PricingPriced, "0.00045")
		if got.PromptTokens != 100 || got.CompletionTokens != 20 || got.TotalTokens != 120 {
			t.Fatalf("tokens = %d/%d/%d", got.PromptTokens, got.CompletionTokens, got.TotalTokens)
		}
	})
	t.Run("with no tokens", func(t *testing.T) {
		t.Parallel()
		got := drainStream(t, &erroringStream{err: boom}, false)
		wantRecord(t, got, usage.OutcomeError, usage.PricingUnknown, "")
	})
}

func TestUsageRecordsAStreamCutByItsTenantQuota(t *testing.T) {
	t.Parallel()
	cut := &middlewares.QuotaError{What: "output_tokens"}

	t.Run("after reporting tokens", func(t *testing.T) {
		t.Parallel()
		got := drainStream(t, &erroringStream{chunks: []*provider.StreamChunk{usageChunk(10, 60)}, err: cut}, false)
		// 10 * 2.50/1e6 + 60 * 10/1e6
		wantRecord(t, got, usage.OutcomeError, usage.PricingPriced, "0.000625")
		if got.StatusCode != 429 || got.RefusalCode != "quota_exceeded" {
			t.Fatalf("status/code = %d/%q, want 429/quota_exceeded", got.StatusCode, got.RefusalCode)
		}
		if got.TotalTokens != 70 {
			t.Fatalf("tokens = %d, want 70", got.TotalTokens)
		}
	})
	t.Run("with no tokens", func(t *testing.T) {
		t.Parallel()
		got := drainStream(t, &erroringStream{err: cut}, false)
		wantRecord(t, got, usage.OutcomeError, usage.PricingUnknown, "")
		if got.StatusCode != 429 || got.RefusalCode != "quota_exceeded" {
			t.Fatalf("status/code = %d/%q, want 429/quota_exceeded", got.StatusCode, got.RefusalCode)
		}
	})
	t.Run("a plain failure keeps 500", func(t *testing.T) {
		t.Parallel()
		got := drainStream(t, &erroringStream{err: errors.New("connection reset")}, false)
		if got.StatusCode != 500 || got.RefusalCode != "" {
			t.Fatalf("status/code = %d/%q, want 500/empty", got.StatusCode, got.RefusalCode)
		}
	})
}

func TestUsageKeepsPrecedenceOverAQuotaCut(t *testing.T) {
	t.Parallel()
	cut := &middlewares.QuotaError{What: "output_tokens"}

	t.Run("a cache replay stays cached at zero", func(t *testing.T) {
		t.Parallel()
		got := drainStream(t, &erroringStream{chunks: []*provider.StreamChunk{usageChunk(10, 60)}, err: cut}, true)
		wantRecord(t, got, usage.OutcomeCached, usage.PricingCached, "0")
		if got.StatusCode != 200 || got.RefusalCode != "" {
			t.Fatalf("status/code = %d/%q, want 200/empty", got.StatusCode, got.RefusalCode)
		}
	})
	t.Run("a guard block followed by a quota error stays blocked", func(t *testing.T) {
		t.Parallel()
		blocked := &guard.BlockedError{Guard: "stream-pii", Phase: guard.PhaseOutput, Reason: "ssn"}
		st := &sequenceStream{chunks: []*provider.StreamChunk{usageChunk(10, 60)}, errs: []error{blocked, cut}}
		got := drainStream(t, st, false)
		wantRecord(t, got, usage.OutcomeBlocked, usage.PricingPriced, "0.000625")
		if got.BlockedBy != "stream-pii" || got.StatusCode != 400 || got.RefusalCode != "" {
			t.Fatalf("blocked by %q, status/code = %d/%q", got.BlockedBy, got.StatusCode, got.RefusalCode)
		}
	})
}

// sequenceStream delivers its chunks, then one error per call from errs.
type sequenceStream struct {
	chunks []*provider.StreamChunk
	errs   []error
	idx    int
}

func (s *sequenceStream) Next(context.Context) (*provider.StreamChunk, error) {
	if s.idx < len(s.chunks) {
		c := s.chunks[s.idx]
		s.idx++
		return c, nil
	}
	i := s.idx - len(s.chunks)
	if i >= len(s.errs) {
		return nil, io.EOF
	}
	s.idx++
	return nil, s.errs[i]
}
func (s *sequenceStream) Close() error           { return nil }
func (s *sequenceStream) Usage() *provider.Usage { return nil }

// drainThroughLifecycle runs a stream request through the usage stage with
// the stream lifecycle stage inside it, as the pipeline orders them, reads
// the stream until it ends or fails, and returns the one record and the last Next
// error. A hung stream fails the test instead of hanging it.
func drainThroughLifecycle(t *testing.T, st provider.Stream, quota middlewares.StreamQuota) (*usage.Record, error) {
	t.Helper()
	rec := newRecordingUsage()
	umw := middlewares.NewUsage(rec, gpt4o, nil)
	lmw := middlewares.NewStreamLifecycle(plugin.NewRegistry(), middlewares.StreamLifecycleConfig{
		QuotaResolver: func(context.Context) middlewares.StreamQuota { return quota },
	})
	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "gpt-4o"},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
	}
	resp, err := umw.Process(context.Background(), req, func(ctx context.Context) (*pipeline.Response, error) {
		return lmw.Process(ctx, req, func(context.Context) (*pipeline.Response, error) {
			req.State[pipeline.StateProviderName] = "openai"
			return &pipeline.Response{Stream: st}, nil
		})
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		for {
			if _, e := resp.Stream.Next(context.Background()); e != nil {
				done <- e
				return
			}
		}
	}()
	var last error
	select {
	case last = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end")
	}
	_ = resp.Stream.Close()
	if err := umw.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return rec.only(t), last
}

// parkedStream delivers its chunks, then blocks in Next until it is closed,
// and answers the close with err (io.EOF when err is nil), as a provider
// body does when something closes it under a reader.
type parkedStream struct {
	chunks []*provider.StreamChunk
	err    error
	idx    int
	closed chan struct{}
	once   sync.Once
}

func newParkedStream(err error, chunks ...*provider.StreamChunk) *parkedStream {
	return &parkedStream{chunks: chunks, err: err, closed: make(chan struct{})}
}

func (s *parkedStream) Next(context.Context) (*provider.StreamChunk, error) {
	if s.idx < len(s.chunks) {
		c := s.chunks[s.idx]
		s.idx++
		return c, nil
	}
	<-s.closed
	if s.err != nil {
		return nil, s.err
	}
	return nil, io.EOF
}
func (s *parkedStream) Close() error           { s.once.Do(func() { close(s.closed) }); return nil }
func (s *parkedStream) Usage() *provider.Usage { return nil }

func TestUsageRecordsAStreamCutByItsTimeLimitWhileParked(t *testing.T) {
	t.Parallel()
	quota := middlewares.StreamQuota{MaxDuration: 20 * time.Millisecond}

	for name, closeErr := range map[string]error{
		"close surfaces as EOF":            nil,
		"close surfaces as a read failure": errors.New("body closed"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st := newParkedStream(closeErr, usageChunk(10, 20))
			got, last := drainThroughLifecycle(t, st, quota)
			if !middlewares.IsQuotaExceeded(last) {
				t.Fatalf("Next ended with %v, want a quota error", last)
			}
			// 10 * 2.50/1e6 + 20 * 10/1e6
			wantRecord(t, got, usage.OutcomeError, usage.PricingPriced, "0.000225")
			if got.StatusCode != 429 || got.RefusalCode != "quota_exceeded" {
				t.Fatalf("status/code = %d/%q, want 429/quota_exceeded", got.StatusCode, got.RefusalCode)
			}
		})
	}
}

func TestUsagePricesTheChunkThatTrippedTheTokenCap(t *testing.T) {
	t.Parallel()
	// The provider stream reports no usage of its own: the only count is on
	// the chunk the lifecycle stage withholds when it cuts the stream.
	st := testutil.NewFakeStream([]*provider.StreamChunk{usageChunk(10, 60)}, nil)
	got, last := drainThroughLifecycle(t, st, middlewares.StreamQuota{MaxTokens: 50})
	if !middlewares.IsQuotaExceeded(last) {
		t.Fatalf("Next ended with %v, want a quota error", last)
	}
	// 10 * 2.50/1e6 + 60 * 10/1e6
	wantRecord(t, got, usage.OutcomeError, usage.PricingPriced, "0.000625")
	if got.StatusCode != 429 || got.RefusalCode != "quota_exceeded" || got.TotalTokens != 70 {
		t.Fatalf("status/code/tokens = %d/%q/%d", got.StatusCode, got.RefusalCode, got.TotalTokens)
	}
}

func TestUsageRecordsAStreamCacheReplay(t *testing.T) {
	t.Parallel()
	st := testutil.NewFakeStream([]*provider.StreamChunk{
		{Delta: provider.Delta{Content: "hi"}},
		usageChunk(12, 4),
	}, nil)
	got := drainStream(t, st, true)
	wantRecord(t, got, usage.OutcomeCached, usage.PricingCached, "0")
	if !got.Cached || got.TotalTokens != 16 {
		t.Fatalf("cached %v, tokens %d", got.Cached, got.TotalTokens)
	}
}

func TestUsageRecordsAStreamGuardBlock(t *testing.T) {
	t.Parallel()
	blocked := &guard.BlockedError{Guard: "stream-pii", Phase: guard.PhaseOutput, Reason: "ssn"}

	t.Run("after reporting tokens", func(t *testing.T) {
		t.Parallel()
		got := drainStream(t, &erroringStream{chunks: []*provider.StreamChunk{usageChunk(100, 20)}, err: blocked}, false)
		wantRecord(t, got, usage.OutcomeBlocked, usage.PricingPriced, "0.00045")
		if got.BlockedBy != "stream-pii" {
			t.Fatalf("blocked by %q", got.BlockedBy)
		}
	})
	t.Run("with no tokens", func(t *testing.T) {
		t.Parallel()
		got := drainStream(t, &erroringStream{err: blocked}, false)
		wantRecord(t, got, usage.OutcomeBlocked, usage.PricingUnknown, "")
		if got.BlockedBy != "stream-pii" {
			t.Fatalf("blocked by %q", got.BlockedBy)
		}
	})
}

func TestUsageRecordsAStreamThatEndsWithoutUsage(t *testing.T) {
	t.Parallel()
	st := testutil.NewFakeStream([]*provider.StreamChunk{{Delta: provider.Delta{Content: "hi"}}}, nil)
	got := drainStream(t, st, false)
	wantRecord(t, got, usage.OutcomeOK, usage.PricingUnknown, "")
}

func TestUsageRecordsAGuardBlockedCacheReplay(t *testing.T) {
	t.Parallel()
	blocked := &guard.BlockedError{Guard: "stream-pii", Phase: guard.PhaseOutput, Reason: "ssn"}
	got := drainStream(t, &erroringStream{chunks: []*provider.StreamChunk{usageChunk(100, 20)}, err: blocked}, true)
	wantRecord(t, got, usage.OutcomeBlocked, usage.PricingCached, "0")
	if got.BlockedBy != "stream-pii" || !got.Cached {
		t.Fatalf("blocked by %q, cached %v", got.BlockedBy, got.Cached)
	}
}

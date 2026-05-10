package middlewares_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
	"github.com/xraph/nexus/usage"
)

type recordingUsage struct {
	mu      sync.Mutex
	records []*usage.Record
	done    chan struct{}
}

func newRecordingUsage() *recordingUsage {
	return &recordingUsage{done: make(chan struct{}, 4)}
}

func (r *recordingUsage) Record(_ context.Context, rec *usage.Record) error {
	r.mu.Lock()
	r.records = append(r.records, rec)
	r.mu.Unlock()
	r.done <- struct{}{}
	return nil
}
func (r *recordingUsage) MonthlySpend(_ context.Context, _ string) (float64, error) { return 0, nil }
func (r *recordingUsage) DailyRequests(_ context.Context, _ string) (int, error)    { return 0, nil }
func (r *recordingUsage) Summary(_ context.Context, _, _ string) (*usage.Summary, error) {
	return nil, nil //nolint:nilnil // unused
}
func (r *recordingUsage) Query(_ context.Context, _ *usage.QueryOptions) ([]*usage.Record, int, error) {
	return nil, 0, nil
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
	mw := middlewares.NewUsage(rec)

	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "gpt-4o"},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
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
}

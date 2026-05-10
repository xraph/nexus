package fallback_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/xraph/nexus/fallback"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// fakeProvider is a stand-in provider whose CompleteStream behaviour is
// configurable per test.
type fakeProvider struct {
	name        string
	streamErr   error
	streamCalls int
	chunks      []*provider.StreamChunk
}

func (p *fakeProvider) Name() string { return p.name }
func (p *fakeProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Streaming: true}
}
func (p *fakeProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (p *fakeProvider) Complete(_ context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, errors.New("not used")
}
func (p *fakeProvider) CompleteStream(_ context.Context, _ *provider.CompletionRequest) (provider.Stream, error) {
	p.streamCalls++
	if p.streamErr != nil {
		return nil, p.streamErr
	}
	return testutil.NewFakeStream(p.chunks, nil), nil
}
func (p *fakeProvider) Embed(_ context.Context, _ *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, errors.New("not used")
}
func (p *fakeProvider) Healthy(_ context.Context) bool { return true }

// TestExecuteStream_FailoverBeforeFirstChunk: primary's CompleteStream fails
// before any chunk; fallback succeeds. Caller sees fallback's stream.
func TestExecuteStream_FailoverBeforeFirstChunk(t *testing.T) {
	t.Parallel()
	primary := &fakeProvider{name: "primary", streamErr: errors.New("upstream down")}
	backup := &fakeProvider{
		name:   "backup",
		chunks: []*provider.StreamChunk{{Delta: provider.Delta{Content: "hi from backup"}, FinishReason: "stop"}},
	}

	policy := &fallback.Policy{MaxRetries: 0, RetryDelay: time.Millisecond, RetryBackoff: 1, Timeout: time.Second}
	svc := fallback.NewService(policy)

	stream, err := svc.ExecuteStream(context.Background(), primary, []provider.Provider{backup}, &provider.CompletionRequest{Model: "m"})
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var got string
	for {
		c, e := stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("Next: %v", e)
		}
		got += c.Delta.Content
	}
	if got != "hi from backup" {
		t.Fatalf("served from = %q, want backup", got)
	}
	if primary.streamCalls != 1 || backup.streamCalls != 1 {
		t.Fatalf("calls: primary=%d backup=%d", primary.streamCalls, backup.streamCalls)
	}
}

// TestExecuteStream_NoFailoverOnSuccess: primary succeeds, fallback never invoked.
func TestExecuteStream_NoFailoverOnSuccess(t *testing.T) {
	t.Parallel()
	primary := &fakeProvider{
		name:   "primary",
		chunks: []*provider.StreamChunk{{Delta: provider.Delta{Content: "primary"}, FinishReason: "stop"}},
	}
	backup := &fakeProvider{
		name:   "backup",
		chunks: []*provider.StreamChunk{{Delta: provider.Delta{Content: "should not run"}}},
	}

	svc := fallback.NewService(fallback.DefaultPolicy())
	stream, err := svc.ExecuteStream(context.Background(), primary, []provider.Provider{backup}, &provider.CompletionRequest{Model: "m"})
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()
	for {
		_, e := stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("Next: %v", e)
		}
	}
	if backup.streamCalls != 0 {
		t.Fatalf("backup invoked %d times — must be 0 when primary succeeds", backup.streamCalls)
	}
}

// TestExecuteStream_AllFail: primary + every fallback fail; aggregate error.
func TestExecuteStream_AllFail(t *testing.T) {
	t.Parallel()
	primary := &fakeProvider{name: "p", streamErr: errors.New("p down")}
	backup := &fakeProvider{name: "b", streamErr: errors.New("b down")}

	policy := &fallback.Policy{MaxRetries: 0, RetryDelay: time.Millisecond, RetryBackoff: 1, Timeout: time.Second}
	svc := fallback.NewService(policy)

	_, err := svc.ExecuteStream(context.Background(), primary, []provider.Provider{backup}, &provider.CompletionRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected aggregate error, got nil")
	}
}

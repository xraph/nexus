package middlewares_test

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/plugin"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/testutil"
)

// TestStreamLifecycle_MaxStreamDuration cancels the stream when the
// per-tenant max-duration quota is exceeded mid-flight.
func TestStreamLifecycle_MaxStreamDuration(t *testing.T) {
	t.Parallel()

	// A stream that never EOFs on its own — cancellation must come from
	// the watchdog.
	stream := &slowStream{interval: 50 * time.Millisecond}

	registry := plugin.NewRegistry()
	cfg := middlewares.StreamLifecycleConfig{
		QuotaResolver: func(_ context.Context) middlewares.StreamQuota {
			return middlewares.StreamQuota{MaxDuration: 150 * time.Millisecond}
		},
	}
	mw := middlewares.NewStreamLifecycle(registry, cfg)

	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "m"},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
	}
	resp, err := mw.Process(context.Background(), req, func(_ context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Stream: stream}, nil
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	start := time.Now()
	for {
		_, e := resp.Stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			t.Fatal("expected quota cancellation, got clean EOF")
		}
		if e != nil {
			break
		}
	}
	elapsed := time.Since(start)
	_ = resp.Stream.Close()

	// Should have been canceled close to 150ms. Allow generous slack for
	// scheduling on busy CI machines.
	if elapsed > 1*time.Second {
		t.Fatalf("watchdog took too long: %v", elapsed)
	}
	if atomic.LoadInt32(&stream.closed) == 0 {
		t.Fatal("inner stream not closed after quota cancellation")
	}
}

// slowStream emits one chunk per interval forever; it returns ctx.Err()
// when its ctx is canceled.
type slowStream struct {
	interval time.Duration
	closed   int32
}

func (s *slowStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(s.interval):
		return &provider.StreamChunk{Delta: provider.Delta{Content: "."}}, nil
	}
}

func (s *slowStream) Close() error {
	atomic.StoreInt32(&s.closed, 1)
	return nil
}
func (s *slowStream) Usage() *provider.Usage { return nil }

// TestStreamLifecycle_NoQuotaResolverNoEnforcement is the negative case:
// without a resolver, streams are never preemptively canceled.
func TestStreamLifecycle_NoQuotaResolverNoEnforcement(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "a"}},
		{Delta: provider.Delta{Content: "b"}, FinishReason: "stop"},
	}
	stream := testutil.NewFakeStream(chunks, nil)

	mw := middlewares.NewStreamLifecycle(plugin.NewRegistry(), middlewares.StreamLifecycleConfig{})
	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "m"},
		Type:       pipeline.RequestStream,
		State:      map[string]any{},
	}
	resp, err := mw.Process(context.Background(), req, func(_ context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Stream: stream}, nil
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	for {
		_, e := resp.Stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("unexpected err: %v", e)
		}
	}
	_ = resp.Stream.Close()
}

// TestTenantStreamQuota reads the stream limits from the tenant the access
// stage loaded, and applies none without one.
func TestTenantStreamQuota(t *testing.T) {
	t.Parallel()

	t.Run("with a tenant", func(t *testing.T) {
		t.Parallel()
		ctx := middlewares.WithTenantForTest(context.Background(), &tenant.Tenant{
			Quota: tenant.Quota{MaxStreamDuration: 2 * time.Second, MaxStreamTokens: 50},
		})
		got := middlewares.TenantStreamQuota(ctx)
		want := middlewares.StreamQuota{MaxDuration: 2 * time.Second, MaxTokens: 50}
		if got != want {
			t.Fatalf("quota = %+v, want %+v", got, want)
		}
	})
	t.Run("without a tenant", func(t *testing.T) {
		t.Parallel()
		if got := middlewares.TenantStreamQuota(context.Background()); got != (middlewares.StreamQuota{}) {
			t.Fatalf("quota = %+v, want none", got)
		}
	})
}

// TestQuotaErrorIsAQuotaExceededRefusal pins the code and status a stream
// cut by its tenant's limit reports.
func TestQuotaErrorIsAQuotaExceededRefusal(t *testing.T) {
	t.Parallel()
	status, code := pipeline.HTTPStatus(&middlewares.QuotaError{What: "output_tokens"})
	if status != 429 || code != pipeline.CodeQuotaExceeded {
		t.Fatalf("status/code = %d/%s, want 429/%s", status, code, pipeline.CodeQuotaExceeded)
	}
}

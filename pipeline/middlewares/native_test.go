package middlewares_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/guard/guards"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func testNative() *provider.NativeContent {
	return &provider.NativeContent{
		Provider: "anthropic",
		Model:    "m",
		Content:  []byte(`[{"type":"thinking","thinking":"a@b.co","signature":"s"},{"type":"text","text":"x"}]`),
	}
}

func completionReq() *pipeline.Request {
	return &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "hi"}}},
		Type:       pipeline.RequestCompletion,
		State:      map[string]any{},
	}
}

func guardedReply(t *testing.T, content string) provider.Message {
	t.Helper()
	svc := guard.NewService()
	svc.Register(guards.NewPII(guard.ActionRedact))
	mw := middlewares.NewGuardrail(svc)
	resp, err := mw.Process(context.Background(), completionReq(), func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Completion: &provider.CompletionResponse{
			Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: content, Native: testNative()}}},
		}}, nil
	})
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}
	return resp.Completion.Choices[0].Message
}

func TestOutputRedactionDropsNative(t *testing.T) {
	t.Parallel()
	msg := guardedReply(t, "mail a@b.co")
	if msg.Content != "mail [EMAIL_REDACTED]" {
		t.Fatalf("content = %v, want the redacted text", msg.Content)
	}
	if msg.Native != nil {
		t.Fatalf("Native = %s, want nil after the guard rewrote the message", msg.Native.Content)
	}
}

func TestUnmodifiedOutputKeepsNative(t *testing.T) {
	t.Parallel()
	msg := guardedReply(t, "nothing to hide")
	if msg.Native == nil {
		t.Fatal("Native dropped although no guard changed the message")
	}
}

func TestResponseCacheNeverStoresNative(t *testing.T) {
	t.Parallel()
	mw := middlewares.NewCache(cache.NewService(stores.NewMemory()))
	miss, err := mw.Process(context.Background(), completionReq(), func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Completion: &provider.CompletionResponse{Model: "m",
			Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "x", Native: testNative()}}}}}, nil
	})
	if err != nil {
		t.Fatalf("miss: %v", err)
	}
	if miss.Completion.Choices[0].Message.Native == nil {
		t.Fatal("the caller's own response lost its Native")
	}
	hit, err := mw.Process(context.Background(), completionReq(), func(context.Context) (*pipeline.Response, error) {
		t.Fatal("expected a cache hit")
		return nil, nil //nolint:nilnil // unreachable
	})
	if err != nil || hit == nil || hit.Completion == nil {
		t.Fatalf("hit = %+v, %v", hit, err)
	}
	if got := hit.Completion.Choices[0].Message; got.Content != "x" || got.Native != nil {
		t.Fatalf("hit message = %+v, want content x without Native", got)
	}
}

func TestStreamCacheNeverStoresNative(t *testing.T) {
	t.Parallel()
	mw := middlewares.NewCache(nil).WithStreamCache(stores.NewMemoryStream(), cache.StreamCacheOptions{Mode: cache.ReplayBurst})
	newReq := func() *pipeline.Request {
		return &pipeline.Request{
			Completion: &provider.CompletionRequest{Model: "m", Stream: true, Messages: []provider.Message{{Role: "user", Content: "hi"}}},
			Type:       pipeline.RequestStream,
			State:      map[string]any{},
		}
	}
	drain := func(s provider.Stream) (sawNative bool) {
		t.Helper()
		defer func() { _ = s.Close() }()
		for {
			c, err := s.Next(context.Background())
			if errors.Is(err, io.EOF) {
				return sawNative
			}
			if err != nil {
				t.Fatalf("Next() error: %v", err)
			}
			if c.Delta.Native != nil {
				sawNative = true
			}
		}
	}

	chunks := []*provider.StreamChunk{
		{Provider: "anthropic", Model: "m", Delta: provider.Delta{Content: "x"}, FinishReason: "stop"},
		{Kind: provider.EventMessageStop, Delta: provider.Delta{Native: testNative()}},
	}
	miss, err := mw.Process(context.Background(), newReq(), func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Stream: testutil.NewFakeStream(chunks, nil)}, nil
	})
	if err != nil {
		t.Fatalf("miss: %v", err)
	}
	if !drain(miss.Stream) {
		t.Fatal("the caller's own stream lost its Native")
	}

	hitReq := newReq()
	hit, err := mw.Process(context.Background(), hitReq, func(context.Context) (*pipeline.Response, error) {
		t.Fatal("expected a stream cache hit")
		return nil, nil //nolint:nilnil // unreachable
	})
	if err != nil || hit == nil || hit.Stream == nil || hitReq.State[pipeline.StateCacheHit] != true {
		t.Fatalf("hit = %+v, %v", hit, err)
	}
	if drain(hit.Stream) {
		t.Fatal("the replayed stream carried Native")
	}
}

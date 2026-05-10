package middlewares_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
	"github.com/xraph/nexus/transform"
)

type redactingTransform struct {
	mu          sync.Mutex
	chunkCalled int
	finalCalled int
	finalText   string
}

func (r *redactingTransform) Name() string           { return "redact" }
func (r *redactingTransform) Phase() transform.Phase { return transform.PhaseOutput }
func (r *redactingTransform) TransformOutput(_ context.Context, _ *provider.CompletionRequest, resp *provider.CompletionResponse) error {
	if len(resp.Choices) > 0 {
		if s, ok := resp.Choices[0].Message.Content.(string); ok {
			resp.Choices[0].Message.Content = strings.ReplaceAll(s, "secret", "[redacted]")
		}
	}
	return nil
}
func (r *redactingTransform) TransformChunk(_ context.Context, _ *provider.CompletionRequest, c *provider.StreamChunk) (*provider.StreamChunk, error) {
	r.mu.Lock()
	r.chunkCalled++
	r.mu.Unlock()
	if c == nil {
		return nil, nil
	}
	out := *c
	out.Delta.Content = strings.ReplaceAll(out.Delta.Content, "secret", "[redacted]")
	return &out, nil
}
func (r *redactingTransform) TransformAccumulated(_ context.Context, _ *provider.CompletionRequest, resp *provider.CompletionResponse) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finalCalled++
	if len(resp.Choices) > 0 {
		if s, ok := resp.Choices[0].Message.Content.(string); ok {
			r.finalText = s
		}
	}
	return nil
}

var _ transform.StreamingOutputTransform = (*redactingTransform)(nil)

func TestTransformMiddleware_StreamingChunkRedaction(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "the secret is "}},
		{Delta: provider.Delta{Content: "42"}, FinishReason: "stop"},
	}
	stream := testutil.NewFakeStream(chunks, &provider.Usage{TotalTokens: 3})

	red := &redactingTransform{}
	reg := transform.NewRegistry()
	reg.Register(red)

	mw := middlewares.NewTransform(reg)
	req := &pipeline.Request{
		Completion: &provider.CompletionRequest{Model: "x"},
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
		t.Fatal("stream lost")
	}

	var collected string
	for {
		c, e := resp.Stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("next: %v", e)
		}
		collected += c.Delta.Content
	}
	_ = resp.Stream.Close()

	if collected != "the [redacted] is 42" {
		t.Fatalf("redacted output = %q", collected)
	}
	red.mu.Lock()
	defer red.mu.Unlock()
	if red.chunkCalled < 2 {
		t.Fatalf("chunkCalled = %d, want >=2", red.chunkCalled)
	}
	if red.finalCalled != 1 {
		t.Fatalf("finalCalled = %d, want 1", red.finalCalled)
	}
	if red.finalText != "the [redacted] is 42" {
		t.Fatalf("finalText = %q", red.finalText)
	}
}

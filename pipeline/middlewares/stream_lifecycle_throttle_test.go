package middlewares_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/plugin"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func TestStreamLifecycle_ThrottlesChunkHook(t *testing.T) {
	t.Parallel()

	chunks := make([]*provider.StreamChunk, 0, 32)
	for i := 0; i < 32; i++ {
		chunks = append(chunks, &provider.StreamChunk{
			Provider: "test",
			Model:    "m",
			Delta:    provider.Delta{Content: "x"},
		})
	}
	stream := testutil.NewFakeStream(chunks, nil)

	ext := &captureExt{}
	registry := plugin.NewRegistry()
	registry.Register(ext)

	mw := middlewares.NewStreamLifecycle(registry, middlewares.StreamLifecycleConfig{
		EmitEveryNChunks: 8,
	})

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
			t.Fatalf("Next: %v", e)
		}
	}
	_ = resp.Stream.Close()

	// 32 chunks with N=8 should fire on chunkCount == 1, 8, 16, 24, 32 → 5 fires.
	if ext.chunks != 5 {
		t.Fatalf("OnChunkReceived fired %d times, want 5", ext.chunks)
	}
}

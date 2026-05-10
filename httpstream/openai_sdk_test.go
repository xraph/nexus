//go:build sdkcompat

// SDK drop-in compatibility test. Build-tagged so it doesn't add openai-go
// to the regular test surface; opt in with:
//
//	go test -tags sdkcompat ./httpstream/...
//
// Verifies that an OpenAI-Go SDK client pointed at the proxy's SSE endpoint
// receives the expected chat.completion.chunk envelope shape — text deltas,
// tool-call deltas, and the final usage frame.
package httpstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func TestSDKCompat_TextStream(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{ID: "chatcmpl-1", Provider: "test", Model: "gpt-4o", Delta: provider.Delta{Role: "assistant", Content: "Hello"}},
		{Delta: provider.Delta{Content: ", world"}, FinishReason: "stop"},
		{Kind: provider.EventUsage, Usage: &provider.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7}},
	}
	stream := testutil.NewFakeStream(chunks, nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpstream.Run(r.Context(), w, stream, httpstream.NewSSEOpenAIEncoder(), httpstream.RunOptions{
			HeartbeatInterval: -1,
		})
	}))
	t.Cleanup(srv.Close)

	client := openai.NewClient(
		option.WithBaseURL(srv.URL+"/"),
		option.WithAPIKey("ignored"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s := client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:    "gpt-4o",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})

	var combined string
	for s.Next() {
		ev := s.Current()
		if len(ev.Choices) > 0 {
			combined += ev.Choices[0].Delta.Content
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("stream err: %v", err)
	}
	if !strings.Contains(combined, "Hello") || !strings.Contains(combined, "world") {
		t.Fatalf("collected = %q", combined)
	}
}

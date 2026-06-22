package anthropic_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/anthropic"
	"github.com/xraph/nexus/testutil"
)

// A request with a valid key must carry the credential on the `x-api-key`
// header (Anthropic's auth scheme). This is the regression guard for the
// "x-api-key header is required" 401: if the header is ever dropped or
// renamed, this fails instead of surfacing a confusing upstream 401.
func TestComplete_SendsAPIKeyHeader(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetCompletion(map[string]any{
		"id":          "msg_1",
		"type":        "message",
		"role":        "assistant",
		"model":       "claude-sonnet-4-5",
		"content":     []map[string]any{{"type": "text", "text": "hi"}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
	})

	p := anthropic.New("sk-ant-secret", anthropic.WithBaseURL(mock.Server.URL))
	_, err := p.Complete(context.Background(), &provider.CompletionRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if got := mock.Ctrl.GetLastHeader().Get("x-api-key"); got != "sk-ant-secret" {
		t.Fatalf("x-api-key header = %q, want %q", got, "sk-ant-secret")
	}
	if got := mock.Ctrl.GetLastHeader().Get("anthropic-version"); got == "" {
		t.Fatal("anthropic-version header missing")
	}
}

// Same guarantee on the streaming path — the original bug report was a stream.
func TestCompleteStream_SendsAPIKeyHeader(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler([]string{
		testutil.AnthropicEventJSON("message_stop", map[string]any{"type": "message_stop"}),
		"",
	}))

	p := anthropic.New("sk-ant-secret", anthropic.WithBaseURL(mock.Server.URL))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()
	for {
		if _, err := stream.Next(context.Background()); err != nil {
			break
		}
	}

	if got := mock.Ctrl.GetLastHeader().Get("x-api-key"); got != "sk-ant-secret" {
		t.Fatalf("x-api-key header = %q, want %q", got, "sk-ant-secret")
	}
}

// An empty API key must fail fast with a clear, typed error and WITHOUT a
// network round-trip — instead of sending an empty `x-api-key` header and
// letting Anthropic answer with a cryptic 401 ("x-api-key header is required").
func TestComplete_EmptyAPIKey_FailsFastBeforeNetwork(t *testing.T) {
	mock := testutil.NewMockServer(t)

	p := anthropic.New("", anthropic.WithBaseURL(mock.Server.URL))
	_, err := p.Complete(context.Background(), &provider.CompletionRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})

	if err == nil {
		t.Fatal("Complete() with empty API key returned nil error, want a clear failure")
	}
	if !errors.Is(err, provider.ErrMissingAPIKey) {
		t.Fatalf("error = %v, want errors.Is(err, provider.ErrMissingAPIKey)", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "api key") {
		t.Fatalf("error %q should mention the API key so the cause is obvious", err)
	}
	if path := mock.Ctrl.GetLastPath(); path != "" {
		t.Fatalf("expected no network call for an empty key, but server was hit at %q", path)
	}
}

// Same fast-fail guarantee on the streaming path.
func TestCompleteStream_EmptyAPIKey_FailsFastBeforeNetwork(t *testing.T) {
	mock := testutil.NewMockServer(t)

	p := anthropic.New("", anthropic.WithBaseURL(mock.Server.URL))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model:    "claude-sonnet-4-5",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err == nil {
		if stream != nil {
			_ = stream.Close()
		}
		t.Fatal("CompleteStream() with empty API key returned nil error, want a clear failure")
	}
	if !errors.Is(err, provider.ErrMissingAPIKey) {
		t.Fatalf("error = %v, want errors.Is(err, provider.ErrMissingAPIKey)", err)
	}
	if path := mock.Ctrl.GetLastPath(); path != "" {
		t.Fatalf("expected no network call for an empty key, but server was hit at %q", path)
	}
}

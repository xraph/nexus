package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/anthropic"
	"github.com/xraph/nexus/testutil"
)

// lastBody decodes the body of the last request the mock server received.
func lastBody(t *testing.T, mock *testutil.MockServer) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.Unmarshal(mock.Ctrl.GetLastBody(), &body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return body
}

// endedStreamHandler answers a stream request with an empty, finished message.
func endedStreamHandler() []string {
	return []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type":    "message_start",
			"message": map[string]any{"id": "msg_s", "model": "m"},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{"type": "message_stop"}),
		"",
	}
}

func completeWith(t *testing.T, mock *testutil.MockServer, p *anthropic.Provider, req *provider.CompletionRequest) {
	t.Helper()
	mock.Ctrl.SetCompletion(anthropicCompletionResp())
	if req.Messages == nil {
		req.Messages = []provider.Message{{Role: "user", Content: "Hello"}}
	}
	if _, err := p.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
}

func streamWith(t *testing.T, mock *testutil.MockServer, p *anthropic.Provider, req *provider.CompletionRequest) {
	t.Helper()
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(endedStreamHandler()))
	if req.Messages == nil {
		req.Messages = []provider.Message{{Role: "user", Content: "Hello"}}
	}
	req.Stream = true
	stream, err := p.CompleteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()
	for {
		if _, err := stream.Next(context.Background()); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("Next() error: %v", err)
			}
			return
		}
	}
}

func maxTokensOf(t *testing.T, body map[string]json.RawMessage) int {
	t.Helper()
	var n int
	if err := json.Unmarshal(body["max_tokens"], &n); err != nil {
		t.Fatalf("decode max_tokens %s: %v", body["max_tokens"], err)
	}
	return n
}

func TestMaxTokensDefaultsPerModel(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		stream    bool
		maxTokens int
		want      int
	}{
		{"opus-5-5 complete", "claude-opus-5-5", false, 0, 16000},
		{"opus-5-5 stream", "claude-opus-5-5", true, 0, 64000},
		{"haiku-4-5 complete", "claude-haiku-4-5", false, 0, 16000},
		{"haiku-4-5 stream", "claude-haiku-4-5", true, 0, 64000},
		{"dated haiku-4-5 stream", "claude-haiku-4-5-20251001", true, 0, 64000},
		{"unlisted complete", "claude-unlisted-9", false, 0, 8192},
		{"unlisted stream", "claude-unlisted-9", true, 0, 8192},
		{"explicit kept", "claude-opus-5-5", false, 300, 300},
		{"explicit kept on stream", "claude-opus-5-5", true, 300, 300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, mock := newMockProvider(t)
			req := &provider.CompletionRequest{Model: tt.model, MaxTokens: tt.maxTokens}
			if tt.stream {
				streamWith(t, mock, p, req)
			} else {
				completeWith(t, mock, p, req)
			}
			if got := maxTokensOf(t, lastBody(t, mock)); got != tt.want {
				t.Errorf("max_tokens = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBudgetThinkingRaisesMaxTokensAboveBudget(t *testing.T) {
	t.Run("budget above max_tokens", func(t *testing.T) {
		p, mock := newMockProvider(t)
		completeWith(t, mock, p, &provider.CompletionRequest{
			Model:     "claude-sonnet-4-5",
			MaxTokens: 4096,
			Thinking:  &provider.ThinkingConfig{Enabled: true, BudgetTokens: 20000},
		})
		body := lastBody(t, mock)
		if got := string(body["thinking"]); got != `{"type":"enabled","budget_tokens":20000}` {
			t.Errorf("thinking = %s", got)
		}
		if got := maxTokensOf(t, body); got != 24096 {
			t.Errorf("max_tokens = %d, want 24096", got)
		}
	})

	t.Run("zero budget becomes the 1024 minimum", func(t *testing.T) {
		p, mock := newMockProvider(t)
		completeWith(t, mock, p, &provider.CompletionRequest{
			Model:     "claude-sonnet-4-5",
			MaxTokens: 4096,
			Thinking:  &provider.ThinkingConfig{Enabled: true},
		})
		body := lastBody(t, mock)
		if got := string(body["thinking"]); got != `{"type":"enabled","budget_tokens":1024}` {
			t.Errorf("thinking = %s", got)
		}
		if got := maxTokensOf(t, body); got != 4096 {
			t.Errorf("max_tokens = %d, want 4096 (already above the budget)", got)
		}
	})

	t.Run("capped at the model's max output", func(t *testing.T) {
		p, mock := newMockProvider(t)
		completeWith(t, mock, p, &provider.CompletionRequest{
			Model:    "claude-sonnet-4-5",
			Thinking: &provider.ThinkingConfig{Enabled: true, BudgetTokens: 62000},
		})
		if got := maxTokensOf(t, lastBody(t, mock)); got != 64000 {
			t.Errorf("max_tokens = %d, want 64000", got)
		}
	})
}

func TestThinkingMapsPerModel(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		thinking *provider.ThinkingConfig
		want     string // "" means the field is absent
	}{
		{"opus-5-5 enabled", "claude-opus-5-5", &provider.ThinkingConfig{Enabled: true}, `{"type":"adaptive"}`},
		{"opus-5-5 off", "claude-opus-5-5", &provider.ThinkingConfig{}, ""},
		{"opus-5-5 nil", "claude-opus-5-5", nil, ""},
		{"fable-5 off", "claude-fable-5", &provider.ThinkingConfig{}, ""},
		{"sonnet-5-5 enabled", "claude-sonnet-5-5", &provider.ThinkingConfig{Enabled: true}, `{"type":"adaptive"}`},
		{"sonnet-5-5 off", "claude-sonnet-5-5", &provider.ThinkingConfig{}, `{"type":"between_tools"}`},
		{"sonnet-5-5 nil", "claude-sonnet-5-5", nil, ""},
		{"opus-4-7 off", "claude-opus-4-7", &provider.ThinkingConfig{}, `{"type":"disabled"}`},
		{"opus-4-7 nil", "claude-opus-4-7", nil, ""},
		{"opus-4-7 enabled", "claude-opus-4-7", &provider.ThinkingConfig{Enabled: true}, `{"type":"adaptive"}`},
		{"opus-4-7 enabled budget ignored", "claude-opus-4-7", &provider.ThinkingConfig{Enabled: true, BudgetTokens: 5000}, `{"type":"adaptive"}`},
		{"adaptive include thinking", "claude-opus-4-7", &provider.ThinkingConfig{Enabled: true, IncludeThinking: true}, `{"type":"adaptive","display":"summarized"}`},
		{"always-on include thinking", "claude-opus-5-5", &provider.ThinkingConfig{Enabled: true, IncludeThinking: true}, `{"type":"adaptive","display":"summarized"}`},
		{"opus-4-5 enabled", "claude-opus-4-5", &provider.ThinkingConfig{Enabled: true, BudgetTokens: 2048}, `{"type":"enabled","budget_tokens":2048}`},
		{"opus-4-5 off", "claude-opus-4-5", &provider.ThinkingConfig{}, ""},
		{"unlisted no budget", "claude-unlisted-9", &provider.ThinkingConfig{Enabled: true}, `{"type":"adaptive"}`},
		{"unlisted budget", "claude-unlisted-9", &provider.ThinkingConfig{Enabled: true, BudgetTokens: 2048}, `{"type":"enabled","budget_tokens":2048}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, mock := newMockProvider(t)
			completeWith(t, mock, p, &provider.CompletionRequest{Model: tt.model, MaxTokens: 8192, Thinking: tt.thinking})
			raw, present := lastBody(t, mock)["thinking"]
			switch {
			case tt.want == "" && present:
				t.Errorf("thinking = %s, want absent", raw)
			case tt.want != "" && string(raw) != tt.want:
				t.Errorf("thinking = %s, want %s", raw, tt.want)
			}
		})
	}
}

func TestEffortGoesToOutputConfig(t *testing.T) {
	t.Run("supported effort is sent", func(t *testing.T) {
		p, mock := newMockProvider(t)
		completeWith(t, mock, p, &provider.CompletionRequest{
			Model:    "claude-opus-5-5",
			Thinking: &provider.ThinkingConfig{Enabled: true, Effort: "high"},
		})
		if got := string(lastBody(t, mock)["output_config"]); got != `{"effort":"high"}` {
			t.Errorf("output_config = %s", got)
		}
	})

	t.Run("no effort, no output_config", func(t *testing.T) {
		p, mock := newMockProvider(t)
		completeWith(t, mock, p, &provider.CompletionRequest{
			Model:    "claude-opus-5-5",
			Thinking: &provider.ThinkingConfig{Enabled: true},
		})
		if raw, ok := lastBody(t, mock)["output_config"]; ok {
			t.Errorf("output_config = %s, want absent", raw)
		}
	})

	for _, tt := range []struct{ model, effort string }{
		{"claude-opus-4-6", "xhigh"},
		{"claude-opus-4-5", "max"},
		{"claude-sonnet-4-5", "low"},
		{"claude-sonnet-4-5-20250929", "high"},
		{"claude-haiku-4-5", "medium"},
	} {
		t.Run(tt.model+" rejects "+tt.effort, func(t *testing.T) {
			for _, stream := range []bool{false, true} {
				p, mock := newMockProvider(t)
				req := &provider.CompletionRequest{
					Model:    tt.model,
					Messages: []provider.Message{{Role: "user", Content: "Hello"}},
					Thinking: &provider.ThinkingConfig{Effort: tt.effort},
				}
				var err error
				if stream {
					_, err = p.CompleteStream(context.Background(), req)
				} else {
					_, err = p.Complete(context.Background(), req)
				}
				if !errors.Is(err, provider.ErrNotSupported) {
					t.Fatalf("stream=%v: err = %v, want one wrapping provider.ErrNotSupported", stream, err)
				}
				if path := mock.Ctrl.GetLastPath(); path != "" {
					t.Fatalf("stream=%v: provider was called (%s)", stream, path)
				}
			}
		})
	}

	t.Run("unlisted model passes effort through", func(t *testing.T) {
		p, mock := newMockProvider(t)
		completeWith(t, mock, p, &provider.CompletionRequest{
			Model:    "foo",
			Thinking: &provider.ThinkingConfig{Effort: "max"},
		})
		if got := string(lastBody(t, mock)["output_config"]); got != `{"effort":"max"}` {
			t.Errorf("output_config = %s", got)
		}
	})
}

func TestFixedSamplingModelsDropTemperature(t *testing.T) {
	temp, topP := 0.2, 0.9
	for _, tt := range []struct {
		model   string
		present bool
	}{
		{"claude-opus-5-5", false},
		{"claude-sonnet-5-5", false},
		{"claude-opus-4-7", false},
		{"claude-sonnet-4-6", true},
		{"claude-haiku-4-5", true},
		{"claude-unlisted-9", true},
	} {
		t.Run(tt.model, func(t *testing.T) {
			p, mock := newMockProvider(t)
			completeWith(t, mock, p, &provider.CompletionRequest{Model: tt.model, Temperature: &temp, TopP: &topP})
			body := lastBody(t, mock)
			for _, key := range []string{"temperature", "top_p"} {
				if _, ok := body[key]; ok != tt.present {
					t.Errorf("%s present = %v, want %v", key, ok, tt.present)
				}
			}
		})
	}
}

func TestAPIVersion20261021DropsSamplingEverywhere(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := anthropic.New("test-key",
		anthropic.WithBaseURL(mock.Server.URL),
		anthropic.WithAPIVersion(anthropic.APIVersion20261021),
	)
	temp, topP := 0.2, 0.9
	completeWith(t, mock, p, &provider.CompletionRequest{Model: "claude-sonnet-4-6", Temperature: &temp, TopP: &topP})

	if got := mock.Ctrl.GetLastHeader().Get("anthropic-version"); got != "2026-10-21" {
		t.Errorf("anthropic-version = %q, want 2026-10-21", got)
	}
	body := lastBody(t, mock)
	for _, key := range []string{"temperature", "top_p"} {
		if raw, ok := body[key]; ok {
			t.Errorf("%s = %s, want absent", key, raw)
		}
	}
}

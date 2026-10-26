package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/anthropic"
)

// toolChoiceServer records the last request body and how many requests came in.
func toolChoiceServer(t *testing.T) (*httptest.Server, *atomic.Int32, *map[string]any) {
	t.Helper()
	var hits atomic.Int32
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		raw, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicCompletionResp())
	}))
	t.Cleanup(srv.Close)
	return srv, &hits, &body
}

func toolChoiceRequest(model string, choice any, withTool bool) *provider.CompletionRequest {
	req := &provider.CompletionRequest{
		Model:      model,
		Messages:   []provider.Message{{Role: "user", Content: "hi"}},
		ToolChoice: choice,
	}
	if withTool {
		req.Tools = []provider.Tool{{Type: "function", Function: provider.ToolFunction{
			Name:       "f",
			Parameters: map[string]any{"type": "object"},
		}}}
	}
	return req
}

func TestAnthropicMapsToolChoice(t *testing.T) {
	fnChoice := map[string]any{"type": "function", "function": map[string]any{"name": "f"}}
	tests := []struct {
		name   string
		choice any
		want   map[string]any
	}{
		{"auto", "auto", map[string]any{"type": "auto"}},
		{"none", "none", map[string]any{"type": "none"}},
		{"required", "required", map[string]any{"type": "any"}},
		{"function", fnChoice, map[string]any{"type": "tool", "name": "f"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _, body := toolChoiceServer(t)
			p := anthropic.New("k", anthropic.WithBaseURL(srv.URL))
			if _, err := p.Complete(context.Background(), toolChoiceRequest("claude-opus-4-8", tt.choice, true)); err != nil {
				t.Fatalf("Complete: %v", err)
			}
			got, _ := json.Marshal((*body)["tool_choice"])
			want, _ := json.Marshal(tt.want)
			if string(got) != string(want) {
				t.Fatalf("tool_choice = %s, want %s", got, want)
			}
		})
	}
}

func TestToolChoiceWithoutToolsIsOmitted(t *testing.T) {
	srv, hits, body := toolChoiceServer(t)
	p := anthropic.New("k", anthropic.WithBaseURL(srv.URL))
	if _, err := p.Complete(context.Background(), toolChoiceRequest("claude-opus-4-8", "auto", false)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
	if _, present := (*body)["tool_choice"]; present {
		t.Fatalf("tool_choice sent without tools: %v", (*body)["tool_choice"])
	}
}

func TestInvalidToolChoiceFailsLocally(t *testing.T) {
	srv, hits, _ := toolChoiceServer(t)
	p := anthropic.New("k", anthropic.WithBaseURL(srv.URL))
	if _, err := p.Complete(context.Background(), toolChoiceRequest("claude-opus-4-8", "sometimes", true)); err == nil {
		t.Fatal("Complete succeeded with an unknown tool_choice")
	}
	if hits.Load() != 0 {
		t.Fatalf("hits = %d, want 0", hits.Load())
	}
}

func TestForcedToolChoiceRefusedOnModelsThatRejectIt(t *testing.T) {
	fnChoice := map[string]any{"type": "function", "function": map[string]any{"name": "f"}}
	for _, model := range []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-1"} {
		for name, choice := range map[string]any{"required": "required", "function": fnChoice} {
			t.Run(model+"/"+name, func(t *testing.T) {
				srv, hits, _ := toolChoiceServer(t)
				p := anthropic.New("k", anthropic.WithBaseURL(srv.URL))
				_, err := p.Complete(context.Background(), toolChoiceRequest(model, choice, true))
				if !errors.Is(err, anthropic.ErrForcedToolChoice) {
					t.Fatalf("err = %v, want ErrForcedToolChoice", err)
				}
				if !errors.Is(err, provider.ErrNotSupported) {
					t.Fatalf("err = %v, want it to wrap provider.ErrNotSupported", err)
				}
				if hits.Load() != 0 {
					t.Fatalf("hits = %d, want 0", hits.Load())
				}
			})
		}
		t.Run(model+"/auto", func(t *testing.T) {
			srv, hits, body := toolChoiceServer(t)
			p := anthropic.New("k", anthropic.WithBaseURL(srv.URL))
			if _, err := p.Complete(context.Background(), toolChoiceRequest(model, "auto", true)); err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if hits.Load() != 1 {
				t.Fatalf("hits = %d, want 1", hits.Load())
			}
			if tc, _ := (*body)["tool_choice"].(map[string]any); tc["type"] != "auto" {
				t.Fatalf("tool_choice = %v, want auto", (*body)["tool_choice"])
			}
		})
	}
}

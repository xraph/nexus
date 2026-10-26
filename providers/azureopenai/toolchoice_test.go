package azureopenai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func TestToolChoiceIsSentInOpenAIShape(t *testing.T) {
	fn := map[string]any{"type": "function", "function": map[string]any{"name": "f"}}
	allowed := map[string]any{"type": "allowed_tools", "allowed_tools": map[string]any{
		"mode":  "auto",
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "f"}}},
	}}
	tests := []struct {
		name string
		in   any
		want any
	}{
		{"allowed_tools passes through", allowed, allowed},
		{"auto", "auto", "auto"},
		{"none", "none", "none"},
		{"required", "required", "required"},
		{"any", "any", "required"},
		{"openai function", fn, fn},
		{"typed required", provider.ToolChoice{Mode: provider.ToolChoiceRequired}, "required"},
		{"typed function", provider.ToolChoice{Mode: provider.ToolChoiceFunction, Function: "f"}, fn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockServer(t)
			p := New("k", WithBaseURL(mock.Server.URL), WithDeploymentID("gpt-4o"))
			_, err := p.Complete(context.Background(), &provider.CompletionRequest{
				Model:      "gpt-4o",
				Messages:   []provider.Message{{Role: "user", Content: "hi"}},
				Tools:      []provider.Tool{{Type: "function", Function: provider.ToolFunction{Name: "f"}}},
				ToolChoice: tt.in,
			})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			var body map[string]any
			if err := json.Unmarshal(mock.Ctrl.GetLastBody(), &body); err != nil {
				t.Fatalf("body: %v", err)
			}
			if got := body["tool_choice"]; !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tool_choice = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAnUnreadableToolChoiceFailsBeforeTheCall(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("k", WithBaseURL(mock.Server.URL), WithDeploymentID("gpt-4o"))
	_, err := p.Complete(context.Background(), &provider.CompletionRequest{
		Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}, ToolChoice: 42,
	})
	if !errors.Is(err, provider.ErrNotSupported) {
		t.Fatalf("err = %v, want an error wrapping provider.ErrNotSupported", err)
	}
	if mock.Ctrl.GetLastBody() != nil {
		t.Fatal("the request reached the server")
	}
}

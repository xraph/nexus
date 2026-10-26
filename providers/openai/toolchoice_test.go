package openai

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func toolChoiceRequest(choice any) *provider.CompletionRequest {
	return &provider.CompletionRequest{
		Model:    "gpt-4o",
		Messages: []provider.Message{{Role: "user", Content: "weather?"}},
		Tools: []provider.Tool{{Type: "function", Function: provider.ToolFunction{
			Name: "get_weather", Parameters: map[string]any{"type": "object"},
		}}},
		ToolChoice: choice,
	}
}

func TestToolChoiceIsSentInOpenAIShape(t *testing.T) {
	fn := map[string]any{"type": "function", "function": map[string]any{"name": "get_weather"}}
	allowed := map[string]any{"type": "allowed_tools", "allowed_tools": map[string]any{
		"mode":  "required",
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "get_weather"}}},
	}}
	custom := map[string]any{"type": "custom", "custom": map[string]any{"name": "grammar_tool"}}
	tests := []struct {
		name string
		in   any
		want any
	}{
		{"auto", "auto", "auto"},
		{"none", "none", "none"},
		{"required", "required", "required"},
		{"any", "any", "required"},
		{"openai function", fn, fn},
		{"anthropic any", map[string]any{"type": "any"}, "required"},
		{"anthropic tool", map[string]any{"type": "tool", "name": "get_weather"}, fn},
		{"typed required", provider.ToolChoice{Mode: provider.ToolChoiceRequired}, "required"},
		{"typed auto", provider.ToolChoice{Mode: provider.ToolChoiceAuto}, "auto"},
		{"typed none pointer", &provider.ToolChoice{Mode: provider.ToolChoiceNone}, "none"},
		{"typed function", provider.ToolChoice{Mode: provider.ToolChoiceFunction, Function: "get_weather"}, fn},
		{"allowed_tools passes through", allowed, allowed},
		{"custom passes through", custom, custom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := testutil.NewMockServer(t)
			p := New("k", WithBaseURL(srv.Server.URL))
			if _, err := p.Complete(context.Background(), toolChoiceRequest(tt.in)); err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if got := lastBody(t, srv)["tool_choice"]; !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tool_choice = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestStreamSendsToolChoiceInOpenAIShape(t *testing.T) {
	srv := testutil.NewMockServer(t)
	p := New("k", WithBaseURL(srv.Server.URL))
	s, err := p.CompleteStream(context.Background(), toolChoiceRequest(provider.ToolChoice{Mode: provider.ToolChoiceRequired}))
	if err == nil {
		_ = s.Close()
	}
	if got := lastBody(t, srv)["tool_choice"]; got != "required" {
		t.Fatalf("tool_choice = %#v, want \"required\"", got)
	}
}

func TestNoToolChoiceSendsNone(t *testing.T) {
	srv := testutil.NewMockServer(t)
	p := New("k", WithBaseURL(srv.Server.URL))
	if _, err := p.Complete(context.Background(), toolChoiceRequest(nil)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, present := lastBody(t, srv)["tool_choice"]; present {
		t.Fatal("tool_choice sent although the request set none")
	}
}

func TestAnUnreadableToolChoiceFailsBeforeTheCall(t *testing.T) {
	srv := testutil.NewMockServer(t)
	p := New("k", WithBaseURL(srv.Server.URL))
	_, err := p.Complete(context.Background(), toolChoiceRequest("sometimes"))
	if !errors.Is(err, provider.ErrNotSupported) {
		t.Fatalf("err = %v, want an error wrapping provider.ErrNotSupported", err)
	}
	if srv.Ctrl.GetLastBody() != nil {
		t.Fatal("the request reached the server")
	}
}

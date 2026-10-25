package anthropic_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/anthropic"
	"github.com/xraph/nexus/testutil"
)

const nativeModel = "claude-opus-5-5"

// signedReplyContent is a reply that interleaves signed thinking with text
// and a tool call, the shape a Claude 5.5 model returns mid tool loop.
func signedReplyContent() []map[string]any {
	return []map[string]any{
		{"type": "thinking", "thinking": "", "signature": "sig1"},
		{"type": "text", "text": "a"},
		{"type": "thinking", "thinking": "", "signature": "sig2"},
		{"type": "tool_use", "id": "t1", "name": "f", "input": map[string]any{"a": 1}},
	}
}

func signedReplyResp() map[string]any {
	return map[string]any{
		"id":          "msg_native",
		"type":        "message",
		"role":        "assistant",
		"model":       nativeModel,
		"content":     signedReplyContent(),
		"stop_reason": "tool_use",
		"usage":       map[string]any{"input_tokens": 10, "output_tokens": 8},
	}
}

// signedReply runs a Complete call against the signed reply and returns the
// assistant message it produced.
func signedReply(t *testing.T) provider.Message {
	t.Helper()
	p, mock := newMockProvider(t)
	mock.Ctrl.SetCompletion(signedReplyResp())
	resp, err := p.Complete(context.Background(), &provider.CompletionRequest{
		Model:    nativeModel,
		Messages: []provider.Message{{Role: "user", Content: "q"}},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	return resp.Choices[0].Message
}

// sentAssistantContent sends history and returns the content of the second
// message in the request body, exactly as it was sent.
func sentAssistantContent(t *testing.T, assistant provider.Message) json.RawMessage {
	t.Helper()
	p, mock := newMockProvider(t)
	mock.Ctrl.SetCompletion(anthropicCompletionResp())
	_, err := p.Complete(context.Background(), &provider.CompletionRequest{
		Model: nativeModel,
		Messages: []provider.Message{
			{Role: "user", Content: "q"},
			assistant,
			{Role: "tool", ToolCallID: "t1", Content: "r"},
			{Role: "user", Content: "next"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	var body struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(mock.Ctrl.GetLastBody(), &body); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if len(body.Messages) != 4 {
		t.Fatalf("sent %d messages, want 4", len(body.Messages))
	}
	if body.Messages[1].Role != "assistant" {
		t.Fatalf("messages[1].role = %q, want assistant", body.Messages[1].Role)
	}
	return body.Messages[1].Content
}

// assertRebuilt checks content is [text want, tool_use t1 {"a":1}] with no
// thinking blocks.
func assertRebuilt(t *testing.T, content json.RawMessage, wantText string, wantInput map[string]any) {
	t.Helper()
	var blocks []map[string]any
	if err := json.Unmarshal(content, &blocks); err != nil {
		t.Fatalf("decode content %s: %v", content, err)
	}
	if len(blocks) != 2 {
		t.Fatalf("content = %s, want a text block and a tool_use block", content)
	}
	for _, b := range blocks {
		if b["type"] == "thinking" || b["type"] == "redacted_thinking" {
			t.Fatalf("rebuilt content kept a thinking block: %s", content)
		}
	}
	if blocks[0]["type"] != "text" || blocks[0]["text"] != wantText {
		t.Errorf("blocks[0] = %v, want text %q", blocks[0], wantText)
	}
	if blocks[1]["type"] != "tool_use" || blocks[1]["id"] != "t1" || blocks[1]["name"] != "f" {
		t.Errorf("blocks[1] = %v, want tool_use t1 f", blocks[1])
	}
	if !reflect.DeepEqual(blocks[1]["input"], wantInput) {
		t.Errorf("blocks[1].input = %v, want %v", blocks[1]["input"], wantInput)
	}
}

func TestCompleteKeepsNativeContentInOrder(t *testing.T) {
	msg := signedReply(t)
	if msg.Native == nil {
		t.Fatal("Message.Native is nil")
	}
	want, err := json.Marshal(signedReplyContent())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(msg.Native.Content, want) {
		t.Errorf("Native.Content = %s\nwant           %s", msg.Native.Content, want)
	}
	if msg.Native.Provider != "anthropic" {
		t.Errorf("Native.Provider = %q, want anthropic", msg.Native.Provider)
	}
	if msg.Native.Model != nativeModel {
		t.Errorf("Native.Model = %q, want %q", msg.Native.Model, nativeModel)
	}
	if msg.Content != "a" || len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "t1" {
		t.Errorf("message fields = %q %v, want text a and tool call t1", msg.Content, msg.ToolCalls)
	}
}

func TestUntouchedAssistantTurnIsSentVerbatim(t *testing.T) {
	msg := signedReply(t)
	sent := sentAssistantContent(t, msg)
	if !bytes.Equal(sent, msg.Native.Content) {
		t.Errorf("messages[1].content = %s\nwant Native.Content  %s", sent, msg.Native.Content)
	}
}

func TestEditedAssistantMessageIsRebuiltWithoutThinking(t *testing.T) {
	msg := signedReply(t)
	msg.Content = "A"
	assertRebuilt(t, sentAssistantContent(t, msg), "A", map[string]any{"a": float64(1)})
}

func TestToolArgsChangedIsAnEdit(t *testing.T) {
	msg := signedReply(t)
	msg.ToolCalls = []provider.ToolCall{{
		ID:       "t1",
		Type:     "function",
		Function: provider.ToolCallFunc{Name: "f", Arguments: `{"a":2}`},
	}}
	assertRebuilt(t, sentAssistantContent(t, msg), "a", map[string]any{"a": float64(2)})
}

func TestNativeFromAnotherProviderIsIgnored(t *testing.T) {
	msg := signedReply(t)
	other := *msg.Native
	other.Provider = "openai"
	msg.Native = &other
	assertRebuilt(t, sentAssistantContent(t, msg), "a", map[string]any{"a": float64(1)})
}

func TestThinkingBlockBindingIsSentOnlyWithAdaptiveThinking(t *testing.T) {
	const beta = "thinking-binding-controls-2026-08-01"
	cases := []struct {
		name     string
		model    string
		thinking *provider.ThinkingConfig
		want     bool
	}{
		{"adaptive", "claude-opus-5-5", &provider.ThinkingConfig{Enabled: true}, true},
		{"budget", "claude-sonnet-4-5", &provider.ThinkingConfig{Enabled: true, BudgetTokens: 2048}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := testutil.NewMockServer(t)
			p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL), anthropic.WithThinkingBlockBinding("drop_block"))
			completeWith(t, mock, p, &provider.CompletionRequest{Model: tc.model, Thinking: tc.thinking})

			gotHeader := false
			for _, v := range mock.Ctrl.GetLastHeader().Values("anthropic-beta") {
				if v == beta {
					gotHeader = true
				}
			}
			if gotHeader != tc.want {
				t.Errorf("anthropic-beta %s sent = %v, want %v", beta, gotHeader, tc.want)
			}

			var thinking struct {
				Type         string `json:"type"`
				BlockBinding *struct {
					PrefixMismatchBehavior string `json:"prefix_mismatch_behavior"`
				} `json:"block_binding"`
			}
			if err := json.Unmarshal(lastBody(t, mock)["thinking"], &thinking); err != nil {
				t.Fatalf("decode thinking: %v", err)
			}
			if got := thinking.BlockBinding != nil; got != tc.want {
				t.Fatalf("block_binding sent = %v, want %v (thinking type %q)", got, tc.want, thinking.Type)
			}
			if tc.want && thinking.BlockBinding.PrefixMismatchBehavior != "drop_block" {
				t.Errorf("prefix_mismatch_behavior = %q, want drop_block", thinking.BlockBinding.PrefixMismatchBehavior)
			}
		})
	}
}

func TestThinkingBlockBindingAcceptsOnlyKnownModes(t *testing.T) {
	cases := []struct {
		mode string
		want string // "" means no block_binding and no beta header
	}{
		{"error", "error"},
		{"drop_block", "drop_block"},
		{"bogus", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run("mode "+tc.mode, func(t *testing.T) {
			mock := testutil.NewMockServer(t)
			p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL), anthropic.WithThinkingBlockBinding(tc.mode))
			completeWith(t, mock, p, &provider.CompletionRequest{Model: "claude-opus-5-5", Thinking: &provider.ThinkingConfig{Enabled: true}})

			var thinking struct {
				BlockBinding *struct {
					PrefixMismatchBehavior string `json:"prefix_mismatch_behavior"`
				} `json:"block_binding"`
			}
			if err := json.Unmarshal(lastBody(t, mock)["thinking"], &thinking); err != nil {
				t.Fatalf("decode thinking: %v", err)
			}
			got := ""
			if thinking.BlockBinding != nil {
				got = thinking.BlockBinding.PrefixMismatchBehavior
			}
			if got != tc.want {
				t.Errorf("prefix_mismatch_behavior = %q, want %q", got, tc.want)
			}
			if beta := mock.Ctrl.GetLastHeader().Get("anthropic-beta"); (beta != "") != (tc.want != "") {
				t.Errorf("anthropic-beta = %q with mode %q", beta, tc.mode)
			}
		})
	}
}

package geminilive

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/xraph/nexus/provider"
)

func TestProvider_ToolCallTranslation(t *testing.T) {
	t.Parallel()

	conn := &fakeConn{
		reads: [][]byte{
			mustJSON(map[string]any{"setupComplete": map[string]any{}}),
			mustJSON(map[string]any{
				"toolCall": map[string]any{
					"functionCalls": []map[string]any{{
						"id":   "fc_1",
						"name": "lookup",
						"args": map[string]any{"q": "weather"},
					}},
				},
			}),
			mustJSON(map[string]any{"serverContent": map[string]any{"turnComplete": true}}),
		},
	}
	p := New("k", withDialer(func(_ context.Context, _, _ string) (wsConn, error) { return conn, nil }))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var toolCalls []provider.ToolCall
	for {
		c, e := stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("Next: %v", e)
		}
		if c.Kind == provider.EventToolCallDelta {
			toolCalls = append(toolCalls, c.Delta.ToolCalls...)
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.ID != "fc_1" || tc.Function.Name != "lookup" {
		t.Fatalf("tool call missing id/name: %+v", tc)
	}
	if tc.Function.Arguments != `{"q":"weather"}` {
		t.Fatalf("arguments = %q", tc.Function.Arguments)
	}
}

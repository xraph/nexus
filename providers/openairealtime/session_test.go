package openairealtime

import (
	"context"
	"testing"

	"github.com/xraph/nexus/provider"
)

func TestProvider_SessionCreatedExposesID(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{
		reads: [][]byte{
			mustJSON(map[string]any{
				"type":    "session.created",
				"session": map[string]any{"id": "sess_abc", "model": "gpt-4o-realtime-preview"},
			}),
			mustJSON(map[string]any{"type": "response.text.delta", "delta": "ok"}),
			mustJSON(map[string]any{"type": "response.done", "response": map[string]any{}}),
		},
	}
	p := New("k", withDialer(func(_ context.Context, _, _, _ string) (wsConn, error) { return conn, nil }))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	rs, ok := stream.(*realtimeStream)
	if !ok {
		t.Fatal("stream type")
	}

	// Drain the MessageStart frame from session.created.
	c, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if c.Kind != provider.EventMessageStart {
		t.Fatalf("first frame kind = %s, want message_start", c.Kind)
	}
	if rs.SessionID() != "sess_abc" {
		t.Fatalf("SessionID = %q, want sess_abc", rs.SessionID())
	}
}

func TestProvider_DropsLifecycleNoise(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{
		reads: [][]byte{
			mustJSON(map[string]any{"type": "rate_limits.updated"}),
			mustJSON(map[string]any{"type": "input_audio_buffer.speech_started"}),
			mustJSON(map[string]any{"type": "response.created"}),
			mustJSON(map[string]any{"type": "response.output_item.added"}),
			mustJSON(map[string]any{"type": "response.text.delta", "delta": "yo"}),
			mustJSON(map[string]any{"type": "response.text.done"}),
			mustJSON(map[string]any{"type": "response.done", "response": map[string]any{}}),
		},
	}
	p := New("k", withDialer(func(_ context.Context, _, _, _ string) (wsConn, error) { return conn, nil }))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	resp, err := provider.Accumulate(context.Background(), stream)
	if err != nil {
		t.Fatalf("Accumulate: %v", err)
	}
	if got := resp.Choices[0].Message.Content; got != "yo" {
		t.Fatalf("content = %q (lifecycle events should not pollute output)", got)
	}
}

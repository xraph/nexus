package openairealtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/xraph/nexus/provider"
)

// fakeConn is a minimal in-process duplex impl of wsConn that reads scripted
// server frames and captures client writes for assertion.
type fakeConn struct {
	mu        sync.Mutex
	reads     [][]byte
	readIdx   int
	writes    [][]byte
	closed    bool
	closeOnce sync.Once
}

func (c *fakeConn) Read(_ context.Context) (websocket.MessageType, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.readIdx >= len(c.reads) {
		return websocket.MessageText, nil, io.EOF
	}
	b := c.reads[c.readIdx]
	c.readIdx++
	return websocket.MessageText, b, nil
}

func (c *fakeConn) Write(_ context.Context, _ websocket.MessageType, b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, append([]byte{}, b...))
	return nil
}

func (c *fakeConn) Close(websocket.StatusCode, string) error {
	c.closeOnce.Do(func() {
		c.closed = true
	})
	return nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestProvider_TextStream(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{
		reads: [][]byte{
			mustJSON(map[string]any{"type": "response.text.delta", "response_id": "r1", "delta": "Hello"}),
			mustJSON(map[string]any{"type": "response.text.delta", "delta": ", world"}),
			mustJSON(map[string]any{"type": "response.done", "response_id": "r1", "response": map[string]any{
				"usage": map[string]any{"input_tokens": 4, "output_tokens": 2, "total_tokens": 6},
			}}),
		},
	}

	p := New("k",
		WithModel("gpt-4o-realtime-preview"),
		withDialer(func(_ context.Context, _, _, _ string) (wsConn, error) { return conn, nil }),
	)

	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{Model: "gpt-4o-realtime-preview"})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	resp, err := provider.Accumulate(context.Background(), stream)
	if err != nil {
		t.Fatalf("Accumulate: %v", err)
	}
	if got := resp.Choices[0].Message.Content; got != "Hello, world" {
		t.Fatalf("content = %q", got)
	}
	if resp.Usage.TotalTokens != 6 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestProvider_AudioStreamAndBiStreamSend(t *testing.T) {
	t.Parallel()

	pcm := []byte{1, 2, 3, 4, 5}
	conn := &fakeConn{
		reads: [][]byte{
			mustJSON(map[string]any{
				"type":  "response.audio.delta",
				"delta": base64.StdEncoding.EncodeToString(pcm),
			}),
			mustJSON(map[string]any{
				"type":  "response.audio_transcript.delta",
				"delta": "hi",
			}),
			mustJSON(map[string]any{"type": "response.done", "response": map[string]any{}}),
		},
	}

	p := New("k", withDialer(func(_ context.Context, _, _, _ string) (wsConn, error) { return conn, nil }))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	bi, ok := stream.(provider.BiStream)
	if !ok {
		t.Fatal("realtimeStream must satisfy BiStream")
	}

	// Forward client audio while consuming server events.
	if sendErr := bi.Send(context.Background(), provider.ClientEvent{
		Type:  "audio_chunk",
		Audio: &provider.AudioChunk{Format: "pcm16", Data: []byte{9, 9, 9}},
	}); sendErr != nil {
		t.Fatalf("send audio: %v", sendErr)
	}
	if sendErr := bi.Send(context.Background(), provider.ClientEvent{Type: "commit"}); sendErr != nil {
		t.Fatalf("send commit: %v", sendErr)
	}

	// Drain server stream, collect audio bytes + transcript via Accumulate.
	resp, err := provider.Accumulate(context.Background(), stream)
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	gotAudio, ok := resp.State["audio"].(provider.AudioChunk)
	if !ok {
		t.Fatalf("audio missing on State: %+v", resp.State)
	}
	if !bytesEqual(gotAudio.Data, pcm) {
		t.Fatalf("audio bytes mismatch: %v vs %v", gotAudio.Data, pcm)
	}
	if gotAudio.Transcript != "hi" {
		t.Fatalf("transcript = %q", gotAudio.Transcript)
	}

	// Verify two writes hit the upstream (audio_chunk → input_audio_buffer.append, commit).
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.writes) != 2 {
		t.Fatalf("got %d writes, want 2", len(conn.writes))
	}

	var firstMsg map[string]any
	if err := json.Unmarshal(conn.writes[0], &firstMsg); err != nil {
		t.Fatalf("decode first write: %v", err)
	}
	if firstMsg["type"] != "input_audio_buffer.append" {
		t.Fatalf("first write type = %v", firstMsg["type"])
	}

	var secondMsg map[string]any
	_ = json.Unmarshal(conn.writes[1], &secondMsg)
	if secondMsg["type"] != "input_audio_buffer.commit" {
		t.Fatalf("second write type = %v", secondMsg["type"])
	}
}

func TestProvider_ErrorEventSurfaces(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{
		reads: [][]byte{
			mustJSON(map[string]any{
				"type":  "error",
				"error": map[string]any{"message": "bad audio format"},
			}),
		},
	}
	p := New("k", withDialer(func(_ context.Context, _, _, _ string) (wsConn, error) { return conn, nil }))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	c, err := stream.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if c.Kind != provider.EventError {
		t.Fatalf("kind = %s", c.Kind)
	}
	if c.Err != "bad audio format" {
		t.Fatalf("err message = %q", c.Err)
	}
}

func TestProvider_CompleteRejected(t *testing.T) {
	t.Parallel()
	p := New("k")
	_, err := p.Complete(context.Background(), &provider.CompletionRequest{})
	if err == nil {
		t.Fatal("Complete must reject")
	}
	if !errors.Is(err, err) { // sanity check that err is non-nil
		t.Fatal("err nil")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

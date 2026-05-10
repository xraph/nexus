package geminilive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/xraph/nexus/provider"
)

type fakeConn struct {
	mu      sync.Mutex
	reads   [][]byte
	readIdx int
	writes  [][]byte
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

func (c *fakeConn) Close(websocket.StatusCode, string) error { return nil }

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func TestProvider_TextThroughTurnComplete(t *testing.T) {
	t.Parallel()

	conn := &fakeConn{
		reads: [][]byte{
			mustJSON(map[string]any{"setupComplete": map[string]any{}}),
			mustJSON(map[string]any{"serverContent": map[string]any{
				"modelTurn": map[string]any{
					"parts": []map[string]any{{"text": "Hello"}},
				},
			}}),
			mustJSON(map[string]any{"serverContent": map[string]any{
				"modelTurn": map[string]any{
					"parts": []map[string]any{{"text": ", world"}},
				},
			}}),
			mustJSON(map[string]any{
				"serverContent": map[string]any{"turnComplete": true},
				"usageMetadata": map[string]any{
					"promptTokenCount":     5,
					"candidatesTokenCount": 2,
					"totalTokenCount":      7,
				},
			}),
		},
	}

	p := New("k", withDialer(func(_ context.Context, _, _ string) (wsConn, error) { return conn, nil }))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{Model: "models/gemini-2.0-flash-exp"})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	resp, err := provider.Accumulate(context.Background(), stream)
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	if got := resp.Choices[0].Message.Content; got != "Hello, world" {
		t.Fatalf("content = %q", got)
	}
	if resp.Usage.TotalTokens != 7 {
		t.Fatalf("usage = %+v", resp.Usage)
	}

	// Verify the setup frame was sent first.
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.writes) < 1 {
		t.Fatal("no writes — setup frame missing")
	}
	var first map[string]any
	if err := json.Unmarshal(conn.writes[0], &first); err != nil {
		t.Fatalf("decode setup: %v", err)
	}
	if _, ok := first["setup"]; !ok {
		t.Fatalf("first write not a setup frame: %v", first)
	}
}

func TestProvider_AudioBiStream(t *testing.T) {
	t.Parallel()

	pcm := []byte{1, 2, 3, 4}
	conn := &fakeConn{
		reads: [][]byte{
			mustJSON(map[string]any{"setupComplete": map[string]any{}}),
			mustJSON(map[string]any{"serverContent": map[string]any{
				"modelTurn": map[string]any{
					"parts": []map[string]any{{
						"inlineData": map[string]any{
							"mimeType": "audio/pcm;rate=24000",
							"data":     base64.StdEncoding.EncodeToString(pcm),
						},
					}},
				},
			}}),
			mustJSON(map[string]any{"serverContent": map[string]any{"turnComplete": true}}),
		},
	}
	p := New("k", withDialer(func(_ context.Context, _, _ string) (wsConn, error) { return conn, nil }))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	bi, ok := stream.(provider.BiStream)
	if !ok {
		t.Fatal("liveStream must satisfy BiStream")
	}
	if sendErr := bi.Send(context.Background(), provider.ClientEvent{
		Type:  "audio_chunk",
		Audio: &provider.AudioChunk{SampleRate: 24000, Data: []byte{9, 9}},
	}); sendErr != nil {
		t.Fatalf("send: %v", sendErr)
	}

	resp, err := provider.Accumulate(context.Background(), stream)
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	gotAudio, ok := resp.State["audio"].(provider.AudioChunk)
	if !ok {
		t.Fatalf("audio not on State: %+v", resp.State)
	}
	if !bytesEqual(gotAudio.Data, pcm) {
		t.Fatalf("audio bytes mismatch")
	}

	// Confirm the audio_chunk send produced a realtimeInput frame.
	conn.mu.Lock()
	defer conn.mu.Unlock()
	var foundRealtime bool
	for _, w := range conn.writes {
		var m map[string]any
		_ = json.Unmarshal(w, &m)
		if _, ok := m["realtimeInput"]; ok {
			foundRealtime = true
			break
		}
	}
	if !foundRealtime {
		t.Fatal("client audio not forwarded as realtimeInput")
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

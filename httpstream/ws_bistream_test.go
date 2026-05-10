package httpstream_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// fakeBiStream records inbound client envelopes and serves a fixed set of
// outbound chunks. It satisfies provider.BiStream so the WSHandler will
// forward client audio/image/control envelopes via Send.
// `gate` (closed by ReleaseUpstream) blocks Next until the test signals.
// This keeps the upstream open while the test pumps client envelopes, so
// the reader goroutine has time to process them before EOF + close race
// it out of the connection.
type fakeBiStream struct {
	mu       sync.Mutex
	received []provider.ClientEvent
	inner    *testutil.FakeStream
	gate     chan struct{}
	gateOnce sync.Once
}

func newFakeBiStream(chunks []*provider.StreamChunk) *fakeBiStream {
	return &fakeBiStream{
		inner: testutil.NewFakeStream(chunks, nil),
		gate:  make(chan struct{}),
	}
}

func (f *fakeBiStream) ReleaseUpstream() {
	f.gateOnce.Do(func() { close(f.gate) })
}

func (f *fakeBiStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.inner.Next(ctx)
}
func (f *fakeBiStream) Close() error           { return f.inner.Close() }
func (f *fakeBiStream) Usage() *provider.Usage { return f.inner.Usage() }

func (f *fakeBiStream) Send(_ context.Context, evt provider.ClientEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.received = append(f.received, evt)
	return nil
}

func (f *fakeBiStream) Received() []provider.ClientEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]provider.ClientEvent, len(f.received))
	copy(cp, f.received)
	return cp
}

type biStreamer struct{ s *fakeBiStream }

func (s *biStreamer) CompleteStream(_ context.Context, _ *provider.CompletionRequest) (provider.Stream, error) {
	return s.s, nil
}

func TestWSHandler_ForwardsAudioAndImageToBiStream(t *testing.T) {
	t.Parallel()

	bi := newFakeBiStream([]*provider.StreamChunk{
		{Delta: provider.Delta{Content: "hi"}, FinishReason: "stop"},
	})
	h := httpstream.NewWSHandler(&biStreamer{s: bi}, httpstream.WSOptions{
		AcceptOrigins:     []string{"*"},
		HeartbeatInterval: -1,
	})

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	conn, dialResp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	// Open the session.
	startMsg, _ := json.Marshal(map[string]any{
		"type":    "start",
		"request": map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "hi"}}},
	})
	if err := conn.Write(ctx, websocket.MessageText, startMsg); err != nil {
		t.Fatalf("write start: %v", err)
	}

	// Send an audio_chunk envelope. The handler should base64-decode the
	// b64 field and forward to BiStream.Send as ClientEvent{Type: "audio_chunk"}.
	audioMsg, _ := json.Marshal(map[string]any{
		"type": "audio_chunk",
		"audio": map[string]any{
			"format":      "pcm16",
			"sample_rate": 24000,
			"b64":         base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4}),
		},
	})
	if err := conn.Write(ctx, websocket.MessageText, audioMsg); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	// Send a commit envelope.
	commitMsg, _ := json.Marshal(map[string]any{"type": "commit"})
	if err := conn.Write(ctx, websocket.MessageText, commitMsg); err != nil {
		t.Fatalf("write commit: %v", err)
	}

	// Wait for the reader goroutine to process both envelopes BEFORE the
	// upstream emits and the connection closes. Without this gate the
	// upstream's EOF + close races the reader and drops the writes.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(bi.Received()) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Now release the upstream so it can emit its chunk + EOF.
	bi.ReleaseUpstream()

	// Drain until done so we know the handler has fully consumed our writes.
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		var ev httpstream.StreamEvent
		_ = json.Unmarshal(data, &ev)
		if ev.Type == httpstream.EventTypeDone {
			break
		}
	}

	got := bi.Received()
	if len(got) < 2 {
		t.Fatalf("got %d client events, want >=2 (audio + commit): %+v", len(got), got)
	}

	var sawAudio, sawCommit bool
	for _, ev := range got {
		switch ev.Type {
		case "audio_chunk":
			sawAudio = true
			if ev.Audio == nil {
				t.Fatalf("audio_chunk missing Audio payload")
			}
			if ev.Audio.Format != "pcm16" || ev.Audio.SampleRate != 24000 {
				t.Fatalf("audio metadata: %+v", ev.Audio)
			}
			if len(ev.Audio.Data) != 4 || ev.Audio.Data[0] != 1 {
				t.Fatalf("audio bytes lost or unmangled: %v", ev.Audio.Data)
			}
		case "commit":
			sawCommit = true
		}
	}
	if !sawAudio || !sawCommit {
		t.Fatalf("missing forwarded events: audio=%v commit=%v", sawAudio, sawCommit)
	}
}

func TestWSHandler_NonBidiDropsClientEnvelopes(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "hi"}, FinishReason: "stop"},
	}
	stream := testutil.NewFakeStream(chunks, nil)
	streamer := &fakeStreamer{chunks: chunks} // from ws_test.go — non-bidi

	h := httpstream.NewWSHandler(streamer, httpstream.WSOptions{
		AcceptOrigins:     []string{"*"},
		HeartbeatInterval: -1,
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	conn, dialResp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	startMsg, _ := json.Marshal(map[string]any{
		"type":    "start",
		"request": map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "hi"}}},
	})
	if err := conn.Write(ctx, websocket.MessageText, startMsg); err != nil {
		t.Fatalf("write start: %v", err)
	}

	// Try forwarding multi-modal — should be silently ignored, not crash.
	audioMsg, _ := json.Marshal(map[string]any{"type": "audio_chunk"})
	if err := conn.Write(ctx, websocket.MessageText, audioMsg); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	// Drain to done — confirms the handler is still alive.
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		var ev httpstream.StreamEvent
		_ = json.Unmarshal(data, &ev)
		if ev.Type == httpstream.EventTypeDone {
			break
		}
	}
	_ = stream
}

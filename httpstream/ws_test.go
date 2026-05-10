package httpstream_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

type fakeStreamer struct {
	chunks []*provider.StreamChunk
}

func (f *fakeStreamer) CompleteStream(_ context.Context, _ *provider.CompletionRequest) (provider.Stream, error) {
	return testutil.NewFakeStream(f.chunks, nil), nil
}

func TestWSHandler_HappyPath(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{Provider: "test", Model: "m", Delta: provider.Delta{Content: "hello"}},
		{Delta: provider.Delta{Content: " world"}, FinishReason: "stop"},
	}
	h := httpstream.NewWSHandler(&fakeStreamer{chunks: chunks}, httpstream.WSOptions{
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

	var combined string
	gotDone := false
	for !gotDone {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		var ev httpstream.StreamEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("decode: %v", err)
		}
		switch ev.Type {
		case httpstream.EventTypeDelta:
			if ev.Delta != nil {
				combined += ev.Delta.Content
			}
		case httpstream.EventTypeDone:
			gotDone = true
		}
	}

	if combined != "hello world" {
		t.Fatalf("got %q, want %q", combined, "hello world")
	}
	if !gotDone {
		t.Fatal("done event never received")
	}
}

func TestWSHandler_AbortClosesPromptly(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "a"}},
		{Delta: provider.Delta{Content: "b"}},
		{Delta: provider.Delta{Content: "c"}, FinishReason: "stop"},
	}
	h := httpstream.NewWSHandler(&fakeStreamer{chunks: chunks}, httpstream.WSOptions{
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

	abortMsg, _ := json.Marshal(map[string]any{"type": "abort"})
	if err := conn.Write(ctx, websocket.MessageText, abortMsg); err != nil {
		t.Fatalf("write abort: %v", err)
	}

	// Eventually the read returns a close frame.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
	t.Fatal("connection did not close after abort")
}

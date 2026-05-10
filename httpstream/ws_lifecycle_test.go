package httpstream_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// gateClosingStream blocks on Next until release is signaled, then EOFs.
// Records its Close.
type gateClosingStream struct {
	release chan struct{}
	closed  atomic.Int32
}

func (g *gateClosingStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	select {
	case <-g.release:
		return nil, context.Canceled // simulates upstream EOF after gate
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (g *gateClosingStream) Close() error {
	g.closed.Add(1)
	return nil
}

func (g *gateClosingStream) Usage() *provider.Usage { return nil }

type gateStreamer struct{ s *gateClosingStream }

func (s *gateStreamer) CompleteStream(_ context.Context, _ *provider.CompletionRequest) (provider.Stream, error) {
	return s.s, nil
}

// TestWSHandler_ServerHeartbeatKeepsConnAlive verifies the WS connection
// stays alive past the heartbeat interval. With HeartbeatInterval=50ms and
// a stream that produces no chunks, a connection that didn't ping would
// drop on idle proxies; here we verify it remains writable after 200ms.
func TestWSHandler_ServerHeartbeatKeepsConnAlive(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	gs := &gateClosingStream{release: gate}
	h := httpstream.NewWSHandler(&gateStreamer{s: gs}, httpstream.WSOptions{
		AcceptOrigins:     []string{"*"},
		HeartbeatInterval: 50 * time.Millisecond,
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

	// Wait past several heartbeat intervals — if the server weren't pinging,
	// idle proxies would have torn the connection. After the wait, send a
	// frame; a successful Write means the connection is still alive.
	time.Sleep(200 * time.Millisecond)

	noop, _ := json.Marshal(map[string]any{"type": "noop"})
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer writeCancel()
	if err := conn.Write(writeCtx, websocket.MessageText, noop); err != nil {
		t.Fatalf("write after heartbeat interval failed (server should be alive): %v", err)
	}

	// Release the upstream so the handler completes.
	close(gate)
}

// TestWSHandler_ClientCloseTearsStream: when the client disconnects
// abruptly, the WS handler should observe ctx cancel via reader and the
// upstream stream's Close must run.
func TestWSHandler_ClientCloseTearsStream(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	gs := &gateClosingStream{release: gate}
	h := httpstream.NewWSHandler(&gateStreamer{s: gs}, httpstream.WSOptions{
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
	startMsg, _ := json.Marshal(map[string]any{
		"type":    "start",
		"request": map[string]any{"model": "m", "messages": []map[string]any{{"role": "user", "content": "hi"}}},
	})
	if err := conn.Write(ctx, websocket.MessageText, startMsg); err != nil {
		t.Fatalf("write start: %v", err)
	}

	// Slam the connection shut without graceful close.
	_ = conn.CloseNow()

	// Wait for the server-side handler to observe ctx cancel and call
	// Close on the upstream stream.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if gs.closed.Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if gs.closed.Load() == 0 {
		t.Fatal("upstream stream's Close was never invoked after client disconnect")
	}
	close(gate)
}

// TestWSHandler_RejectsNonStartFirstFrame verifies the server closes the
// connection if the client doesn't send a `start` envelope first.
func TestWSHandler_RejectsNonStartFirstFrame(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{{Delta: provider.Delta{Content: "hi"}, FinishReason: "stop"}}
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

	// Send something that's not a start envelope.
	bogus, _ := json.Marshal(map[string]any{"type": "abort"})
	if writeErr := conn.Write(ctx, websocket.MessageText, bogus); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}

	// Server should close the connection.
	if _, _, readErr := conn.Read(ctx); readErr == nil {
		t.Fatal("expected close from server")
	}
	_ = testutil.NewMockServer // silence unused import in some build configs
}

package httpstream_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

// failingStreamer fails to start a stream with err.
type failingStreamer struct{ err error }

func (f failingStreamer) CompleteStream(context.Context, *provider.CompletionRequest) (provider.Stream, error) {
	return nil, f.err
}

// wsFailure starts a stream that fails with err and returns the error frame
// the client saw, the close status, and what OnError was given.
func wsFailure(t *testing.T, err error) (frame httpstream.StreamEvent, closed websocket.StatusCode, hooked error) {
	t.Helper()
	var mu sync.Mutex
	h := httpstream.NewWSHandler(failingStreamer{err}, httpstream.WSOptions{
		AcceptOrigins:     []string{"*"},
		HeartbeatInterval: -1,
		OnError: func(_ context.Context, e error) {
			mu.Lock()
			defer mu.Unlock()
			hooked = e
		},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	conn, resp, dialErr := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if dialErr != nil {
		t.Fatalf("dial: %v", dialErr)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	start, _ := json.Marshal(map[string]any{"type": "start", "request": map[string]any{"model": "m"}})
	if werr := conn.Write(ctx, websocket.MessageText, start); werr != nil {
		t.Fatalf("write: %v", werr)
	}
	_, data, rerr := conn.Read(ctx)
	if rerr != nil {
		t.Fatalf("read error frame: %v", rerr)
	}
	if uerr := json.Unmarshal(data, &frame); uerr != nil {
		t.Fatal(uerr)
	}
	_, _, rerr = conn.Read(ctx)
	closed = websocket.CloseStatus(rerr)
	// OnError runs after the close frame is sent: give it a moment.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := hooked != nil
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	return frame, closed, hooked
}

func TestWSHandler_ARefusalClosesWithPolicyViolation(t *testing.T) {
	t.Parallel()
	refusal := &pipeline.RefusalError{Code: pipeline.CodeForbidden, Status: 403, Message: "no scope"}
	frame, closed, hooked := wsFailure(t, refusal)
	if closed != websocket.StatusPolicyViolation {
		t.Fatalf("close status %d, want 1008", closed)
	}
	if frame.Err == nil || frame.Err.Type != "refused" || frame.Err.Code != "forbidden" {
		t.Fatalf("error frame %+v, want refused/forbidden", frame.Err)
	}
	if !errors.Is(hooked, refusal) {
		t.Fatalf("OnError got %v, want the refusal", hooked)
	}
}

func TestWSHandler_AnInternalFailureClosesWithInternalErrorAndIsHooked(t *testing.T) {
	t.Parallel()
	cause := errors.New("POST https://api.example.com?key=secret: boom")
	frame, closed, hooked := wsFailure(t, cause)
	if closed != websocket.StatusInternalError {
		t.Fatalf("close status %d, want 1011", closed)
	}
	if frame.Err == nil || frame.Err.Message != "upstream error" {
		t.Fatalf("error frame %+v, want the fixed message", frame.Err)
	}
	if !errors.Is(hooked, cause) {
		t.Fatalf("OnError got %v, want the real cause", hooked)
	}
}

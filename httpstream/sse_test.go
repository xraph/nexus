package httpstream_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func TestSSEOpenAIEncoder_ForwardsToolCallDeltas(t *testing.T) {
	t.Parallel()
	enc := httpstream.NewSSEOpenAIEncoder()

	rec := httptest.NewRecorder()
	ev := &httpstream.StreamEvent{
		Type:  httpstream.EventTypeToolCall,
		ID:    "id-1",
		Model: "gpt-4o",
		Delta: &provider.Delta{
			ToolCalls: []provider.ToolCall{{
				ID:       "call_1",
				Type:     "function",
				Function: provider.ToolCallFunc{Name: "lookup", Arguments: `{"q":"hi"}`},
			}},
		},
	}
	if err := enc.EncodeEvent(rec.Body, ev); err != nil {
		t.Fatalf("encode: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"tool_calls"`) {
		t.Fatalf("tool_calls not forwarded: %s", body)
	}
	if !strings.Contains(body, `"call_1"`) || !strings.Contains(body, `"lookup"`) || !strings.Contains(body, `{\"q\":\"hi\"}`) {
		t.Fatalf("tool call payload mangled: %s", body)
	}
}

func TestSSEOpenAIEncoder_FinalUsageChunkAndDone(t *testing.T) {
	t.Parallel()
	enc := httpstream.NewSSEOpenAIEncoder()

	rec := httptest.NewRecorder()
	usageEv := &httpstream.StreamEvent{
		Type:  httpstream.EventTypeUsage,
		ID:    "id-1",
		Model: "gpt-4o",
		Usage: &provider.Usage{PromptTokens: 5, CompletionTokens: 7, TotalTokens: 12},
	}
	if err := enc.EncodeEvent(rec.Body, usageEv); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := enc.End(rec.Body); err != nil {
		t.Fatalf("end: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12`) {
		t.Fatalf("usage not emitted: %s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") {
		t.Fatalf("missing [DONE] sentinel: %q", body)
	}
}

func TestSSEOpenAIEncoder_ErrorEventThenDone(t *testing.T) {
	t.Parallel()
	enc := httpstream.NewSSEOpenAIEncoder()
	rec := httptest.NewRecorder()
	werr := &httpstream.WireError{Message: "boom", Type: "upstream", Retryable: false}
	if err := enc.EncodeError(rec.Body, werr); err != nil {
		t.Fatalf("encode error: %v", err)
	}
	if err := enc.End(rec.Body); err != nil {
		t.Fatalf("end: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("missing event: error: %q", body)
	}
	if !strings.Contains(body, `"error":{"message":"boom"`) {
		t.Fatalf("missing error payload: %q", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("missing [DONE] terminator: %q", body)
	}
}

func TestRunner_HappyPathWritesChunksAndDone(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{ID: "id-1", Provider: "test", Model: "m", Delta: provider.Delta{Role: "assistant", Content: "Hi"}},
		{Delta: provider.Delta{Content: "!"}, FinishReason: "stop"},
	}
	stream := testutil.NewFakeStream(chunks, &provider.Usage{TotalTokens: 3})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpstream.Run(r.Context(), w, stream, httpstream.NewSSEOpenAIEncoder(), httpstream.RunOptions{
			HeartbeatInterval: -1,
		})
	}))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	body, _ := io.ReadAll(resp.Body)
	got := string(body)
	if !strings.Contains(got, `"content":"Hi"`) || !strings.Contains(got, `"content":"!"`) {
		t.Fatalf("chunks missing: %s", got)
	}
	if !strings.Contains(got, "data: [DONE]") {
		t.Fatalf("missing terminator: %s", got)
	}
	if !stream.Closed() {
		t.Fatal("stream not closed")
	}
}

func TestRunner_HeartbeatFiresWhenIdle(t *testing.T) {
	t.Parallel()

	// A stream that delivers one chunk after a delay, then EOF.
	gate := make(chan struct{})
	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "ok"}, FinishReason: "stop"},
	}
	stream := &gatedStream{FakeStream: testutil.NewFakeStream(chunks, nil), gate: gate}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpstream.Run(r.Context(), w, stream, httpstream.NewSSEOpenAIEncoder(), httpstream.RunOptions{
			HeartbeatInterval: 50 * time.Millisecond,
		})
	}))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	// Read up to a heartbeat — should arrive within ~150ms.
	pingCh := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 && strings.Contains(string(buf[:n]), ": ping") {
				select {
				case pingCh <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-pingCh:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat not delivered within 2s")
	}
	close(gate)
}

func TestRunner_ContextCancelClosesStream(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	stream := &gatedStream{
		FakeStream: testutil.NewFakeStream([]*provider.StreamChunk{
			{Delta: provider.Delta{Content: "first"}},
		}, nil),
		gate: gate,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpstream.Run(r.Context(), w, stream, httpstream.NewSSEOpenAIEncoder(), httpstream.RunOptions{
			HeartbeatInterval: -1,
		})
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}

	// Allow the handler to observe ctx cancellation and clean up.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if stream.Closed() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !stream.Closed() {
		t.Fatal("stream not closed after ctx cancel")
	}
	close(gate)
}

// gatedStream blocks Next forever so the runner is forced into idle/heartbeat
// territory or the test can drive cancellation behaviour.
type gatedStream struct {
	*testutil.FakeStream
	gate chan struct{}
}

func (g *gatedStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	select {
	case <-g.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return g.FakeStream.Next(ctx)
}

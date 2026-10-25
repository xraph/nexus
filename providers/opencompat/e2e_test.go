package opencompat_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/opencompat"
	"github.com/xraph/nexus/testutil"
)

const compatName = "openai-compatible"

// endpoint resolves every request to one tenant key and base URL, the way a
// host serves a workspace's own OpenAI-compatible server.
type endpoint provider.Credential

func (e endpoint) ResolveCredential(context.Context, string) (provider.Credential, error) {
	return provider.Credential(e), nil
}

func (e endpoint) GetCredential(context.Context, string) (string, error) { return e.APIKey, nil }

// compatGateway is a gateway with one opencompat provider that has no base
// URL or key of its own; every request brings them through cp.
func compatGateway(t *testing.T, cp provider.CredentialProvider) *nexus.Gateway {
	t.Helper()
	opts := []nexus.Option{nexus.WithProvider(opencompat.New(compatName, "", ""))}
	if cp != nil {
		opts = append(opts, nexus.WithCredentialProvider(cp))
	}
	gw := nexus.New(opts...)
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = gw.Shutdown(context.Background()) })
	return gw
}

func toolDelta(t *testing.T, call map[string]any, finish any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"id": "chatcmpl-1", "object": "chat.completion.chunk", "model": "local-model",
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{call}}, "finish_reason": finish}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var getTool = []provider.Tool{{Type: "function", Function: provider.ToolFunction{Name: "get", Parameters: map[string]any{"type": "object"}}}}

func TestOpenCompatToolsAndStreamingThroughTheGateway(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStreamHandler(testutil.OpenAIStreamHandler([]string{
		toolDelta(t, map[string]any{"index": 0, "id": "c1", "type": "function", "function": map[string]any{"name": "get", "arguments": `{"x"`}}, nil),
		toolDelta(t, map[string]any{"index": 0, "function": map[string]any{"arguments": `:1}`}}, "tool_calls"),
	}))
	gw := compatGateway(t, endpoint{APIKey: "t", BaseURL: mock.Server.URL})
	ctx := context.Background()

	stream, err := gw.Engine().CompleteStream(ctx, &provider.CompletionRequest{
		Provider: compatName, Model: "local-model", Stream: true, Tools: getTool,
		Messages: []provider.Message{{Role: "user", Content: "x?"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()
	resp, err := provider.Accumulate(ctx, stream)
	if err != nil {
		t.Fatalf("Accumulate: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %+v", resp.Choices)
	}
	calls := resp.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].ID != "c1" || calls[0].Function.Name != "get" || calls[0].Function.Arguments != `{"x":1}` {
		t.Fatalf("tool calls = %+v, want one c1 get {\"x\":1}", calls)
	}
	if got := mock.Ctrl.GetLastHeader().Get("Authorization"); got != "Bearer t" {
		t.Fatalf("Authorization = %q, want Bearer t", got)
	}
	if !strings.Contains(string(mock.Ctrl.GetLastBody()), `"tools"`) {
		t.Fatalf("body has no tools: %s", mock.Ctrl.GetLastBody())
	}

	if _, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{
		Provider: compatName, Model: "local-model", Tools: getTool, ToolChoice: "required",
		Messages: []provider.Message{{Role: "user", Content: "x?"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(mock.Ctrl.GetLastBody(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["tool_choice"] != "required" {
		t.Fatalf("tool_choice = %v, want required sent through unchanged", body["tool_choice"])
	}
}

func TestOpenCompatEmbedsThroughTheGateway(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetEmbedding(json.RawMessage(testutil.DefaultEmbeddingResponse()))
	gw := compatGateway(t, endpoint{APIKey: "t", BaseURL: mock.Server.URL})

	resp, err := gw.Engine().Embed(context.Background(), &provider.EmbeddingRequest{
		Provider: compatName, Model: "local-embed", Input: []string{"hello"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Embeddings) != 1 || len(resp.Embeddings[0]) != 5 || resp.Provider != compatName {
		t.Fatalf("response = %+v", resp)
	}
	if mock.Ctrl.GetLastPath() != "/embeddings" || mock.Ctrl.GetLastHeader().Get("Authorization") != "Bearer t" {
		t.Fatalf("mock saw path %q auth %q", mock.Ctrl.GetLastPath(), mock.Ctrl.GetLastHeader().Get("Authorization"))
	}
}

func TestOpenCompatWithoutAnyBaseURLFails(t *testing.T) {
	gw := compatGateway(t, nil)
	_, err := gw.Engine().Complete(context.Background(), &provider.CompletionRequest{
		Provider: compatName, Model: "local-model", Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, provider.ErrMissingBaseURL) {
		t.Fatalf("err = %v, want ErrMissingBaseURL", err)
	}
}

func TestATenantKeyNeverReachesTheGatewaysError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"bad key %s"}}`, r.Header.Get("Authorization"))
	}))
	t.Cleanup(srv.Close)
	gw := compatGateway(t, endpoint{APIKey: "sk-workspace-secret", BaseURL: srv.URL})

	_, err := gw.Engine().Complete(context.Background(), &provider.CompletionRequest{
		Provider: compatName, Model: "local-model", Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("want the 401 as an error")
	}
	if strings.Contains(err.Error(), "sk-workspace-secret") {
		t.Fatalf("error carries the tenant key: %v", err)
	}
}

func TestOpenCompatAcceptsCredentials(t *testing.T) {
	var p provider.Provider = opencompat.New(compatName, "", "")
	aware, ok := p.(provider.CredentialAware)
	if !ok || !aware.AcceptsCredentials() {
		t.Fatal("opencompat must accept tenant credentials")
	}
}

func TestOpenCompatWithTimeoutBoundsTheHeaders(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-release:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(testutil.DefaultCompletionResponse())
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	p := opencompat.New(compatName, srv.URL, "k", opencompat.WithTimeout(50*time.Millisecond))
	start := time.Now()
	_, err := p.Complete(context.Background(), &provider.CompletionRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("err = %v, want a response-header timeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("returned after %v, want within 1s", elapsed)
	}
}

func TestOpenCompatStreamOutlivesTheResponseHeaderTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for i := range 5 {
			time.Sleep(40 * time.Millisecond)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", testutil.OpenAIChunkJSON("id-1", "m", fmt.Sprintf("chunk%d", i), ""))
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(srv.Close)

	p := opencompat.New(compatName, srv.URL, "k", opencompat.WithTimeout(50*time.Millisecond))
	ctx := context.Background()
	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{Model: "m", Stream: true, Messages: []provider.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()
	resp, err := provider.Accumulate(ctx, stream)
	if err != nil {
		t.Fatalf("Accumulate: %v", err)
	}
	if got := resp.Choices[0].Message.Content; got != "chunk0chunk1chunk2chunk3chunk4" {
		t.Fatalf("content = %v, want all five chunks", got)
	}
}

func TestOpenCompatChosenBaseURLGoesKeyless(t *testing.T) {
	configured := testutil.NewMockServer(t)
	chosen := testutil.NewMockServer(t)
	gw := nexus.New(
		nexus.WithProvider(opencompat.New(compatName, configured.Server.URL, "sk-operator-global")),
		nexus.WithCredentialProvider(endpoint{BaseURL: chosen.Server.URL}),
	)
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = gw.Shutdown(context.Background()) })

	if _, err := gw.Engine().Complete(context.Background(), &provider.CompletionRequest{
		Provider: compatName, Model: "local-model", Messages: []provider.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got, sent := chosen.Ctrl.GetLastHeader()["Authorization"]; sent {
		t.Fatalf("the chosen host got an Authorization header %q, want a keyless request", got)
	}
	if configured.Ctrl.GetLastPath() != "" {
		t.Fatal("the configured base URL was called")
	}
}

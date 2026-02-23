package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providertest"
	"github.com/xraph/nexus/testutil"
)

// ---------------------------------------------------------------------------
// Constructor & basic accessors
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	p := New("sk-test-key")
	if p == nil {
		t.Fatal("New() returned nil")
	}
	if p.apiKey != "sk-test-key" {
		t.Errorf("apiKey = %q, want %q", p.apiKey, "sk-test-key")
	}
	if p.baseURL != "https://api.openai.com/v1" {
		t.Errorf("default baseURL = %q, want %q", p.baseURL, "https://api.openai.com/v1")
	}
	if p.client == nil {
		t.Error("client must not be nil after New()")
	}
}

func TestNewWithOptions(t *testing.T) {
	p := New("sk-key",
		WithBaseURL("https://custom.example.com/v1"),
		WithOrgID("org-abc"),
	)
	if p.baseURL != "https://custom.example.com/v1" {
		t.Errorf("baseURL = %q, want %q", p.baseURL, "https://custom.example.com/v1")
	}
	if p.orgID != "org-abc" {
		t.Errorf("orgID = %q, want %q", p.orgID, "org-abc")
	}
}

func TestName(t *testing.T) {
	p := New("key")
	if got := p.Name(); got != "openai" {
		t.Errorf("Name() = %q, want %q", got, "openai")
	}
}

func TestCapabilities(t *testing.T) {
	caps := New("key").Capabilities()

	tests := []struct {
		name string
		got  bool
	}{
		{"Chat", caps.Chat},
		{"Streaming", caps.Streaming},
		{"Embeddings", caps.Embeddings},
		{"Vision", caps.Vision},
		{"Tools", caps.Tools},
		{"JSON", caps.JSON},
		{"Images", caps.Images},
		{"Thinking", caps.Thinking},
		{"Batch", caps.Batch},
	}
	for _, tc := range tests {
		if !tc.got {
			t.Errorf("capability %s should be true", tc.name)
		}
	}

	// Audio is the one capability OpenAI provider does NOT declare.
	if caps.Audio {
		t.Error("Audio capability should be false for openai provider")
	}
}

// ---------------------------------------------------------------------------
// provider.Provider interface compile-time check (also in source, but verify)
// ---------------------------------------------------------------------------

var _ provider.Provider = (*Provider)(nil)

// ---------------------------------------------------------------------------
// Complete (mock server)
// ---------------------------------------------------------------------------

func TestComplete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key", WithBaseURL(mock.Server.URL))

	ctx := context.Background()
	resp, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: "user", Content: "Say hello"},
		},
		MaxTokens: 50,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp == nil {
		t.Fatal("Complete() returned nil response")
	}
	if resp.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", resp.Provider, "openai")
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected at least one choice")
	}
	if resp.Choices[0].Message.Content == nil || resp.Choices[0].Message.Content == "" {
		t.Error("expected non-empty content in first choice")
	}
	if resp.Usage.TotalTokens == 0 {
		t.Error("expected non-zero TotalTokens")
	}
	if resp.Latency <= 0 {
		t.Error("expected positive latency")
	}

	// Verify the request was sent correctly.
	if got := mock.Ctrl.GetLastPath(); got != "/chat/completions" {
		t.Errorf("request path = %q, want %q", got, "/chat/completions")
	}
	hdr := mock.Ctrl.GetLastHeader()
	if got := hdr.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer test-key")
	}
	if got := hdr.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type header = %q, want %q", got, "application/json")
	}

	// Verify request body has stream=false.
	var reqBody map[string]any
	if err := json.Unmarshal(mock.Ctrl.GetLastBody(), &reqBody); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if reqBody["model"] != "gpt-4o" {
		t.Errorf("request model = %v, want %q", reqBody["model"], "gpt-4o")
	}
}

func TestComplete_SystemPrompt(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key", WithBaseURL(mock.Server.URL))

	ctx := context.Background()
	_, err := p.Complete(ctx, &provider.CompletionRequest{
		Model:  "gpt-4o",
		System: "You are a helpful assistant.",
		Messages: []provider.Message{
			{Role: "user", Content: "Hi"},
		},
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	// Verify system prompt was prepended as a message.
	var reqBody struct {
		Messages []openAIMessage `json:"messages"`
	}
	if err := json.Unmarshal(mock.Ctrl.GetLastBody(), &reqBody); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if len(reqBody.Messages) < 2 {
		t.Fatalf("expected at least 2 messages (system + user), got %d", len(reqBody.Messages))
	}
	if reqBody.Messages[0].Role != "system" {
		t.Errorf("first message role = %q, want %q", reqBody.Messages[0].Role, "system")
	}
}

func TestComplete_OrgIDHeader(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key", WithBaseURL(mock.Server.URL), WithOrgID("org-xyz"))

	ctx := context.Background()
	_, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: "user", Content: "test"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	hdr := mock.Ctrl.GetLastHeader()
	if got := hdr.Get("OpenAI-Organization"); got != "org-xyz" {
		t.Errorf("OpenAI-Organization header = %q, want %q", got, "org-xyz")
	}
}

func TestComplete_APIError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStatusCode(http.StatusTooManyRequests)
	p := New("test-key", WithBaseURL(mock.Server.URL))

	ctx := context.Background()
	_, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: "user", Content: "test"},
		},
	})
	if err == nil {
		t.Fatal("expected error for 429 status")
	}
}

// ---------------------------------------------------------------------------
// CompleteStream (mock SSE)
// ---------------------------------------------------------------------------

func TestCompleteStream(t *testing.T) {
	mock := testutil.NewMockServer(t)

	chunks := []string{
		testutil.OpenAIChunkJSON("chatcmpl-1", "gpt-4o", "Hello", ""),
		testutil.OpenAIChunkJSON("chatcmpl-1", "gpt-4o", " world", ""),
		testutil.OpenAIChunkJSON("chatcmpl-1", "gpt-4o", "!", "stop"),
		testutil.OpenAIChunkJSONWithUsage("chatcmpl-1", "gpt-4o", 5, 3, 8),
	}
	mock.Ctrl.SetStreamHandler(testutil.OpenAIStreamHandler(chunks))

	p := New("test-key", WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var contents []string
	var lastFinish string
	for {
		chunk, err := stream.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		if chunk.Provider != "openai" {
			t.Errorf("chunk Provider = %q, want %q", chunk.Provider, "openai")
		}
		if chunk.Delta.Content != "" {
			contents = append(contents, chunk.Delta.Content)
		}
		if chunk.FinishReason != "" {
			lastFinish = chunk.FinishReason
		}
	}

	if got := joinStrings(contents); got != "Hello world!" {
		t.Errorf("streamed content = %q, want %q", got, "Hello world!")
	}
	if lastFinish != "stop" {
		t.Errorf("last finish_reason = %q, want %q", lastFinish, "stop")
	}

	// Usage should have been captured from the final chunk.
	usage := stream.Usage()
	if usage == nil {
		t.Fatal("expected non-nil usage after stream completes")
	}
	if usage.PromptTokens != 5 {
		t.Errorf("PromptTokens = %d, want 5", usage.PromptTokens)
	}
	if usage.CompletionTokens != 3 {
		t.Errorf("CompletionTokens = %d, want 3", usage.CompletionTokens)
	}
	if usage.TotalTokens != 8 {
		t.Errorf("TotalTokens = %d, want 8", usage.TotalTokens)
	}
}

func TestCompleteStream_APIError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStatusCode(http.StatusInternalServerError)
	p := New("test-key", WithBaseURL(mock.Server.URL))

	ctx := context.Background()
	_, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		Stream: true,
	})
	if err == nil {
		t.Fatal("expected error for 500 status on stream")
	}
}

// ---------------------------------------------------------------------------
// Embed (mock server)
// ---------------------------------------------------------------------------

func TestEmbed(t *testing.T) {
	mock := testutil.NewMockServer(t)

	var embResp struct {
		Object string `json:"object"`
		Data   []struct {
			Object    string    `json:"object"`
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Model string `json:"model"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(testutil.DefaultEmbeddingResponse(), &embResp); err != nil {
		t.Fatalf("unmarshal default embedding response: %v", err)
	}
	mock.Ctrl.SetEmbedding(embResp)

	p := New("test-key", WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	resp, err := p.Embed(ctx, &provider.EmbeddingRequest{
		Model: "text-embedding-3-small",
		Input: []string{"Hello world"},
	})
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if resp == nil {
		t.Fatal("Embed() returned nil")
	}
	if resp.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", resp.Provider, "openai")
	}
	if len(resp.Embeddings) == 0 {
		t.Fatal("expected at least one embedding vector")
	}
	if len(resp.Embeddings[0]) != 5 {
		t.Errorf("embedding dimension = %d, want 5", len(resp.Embeddings[0]))
	}
	if resp.Usage.PromptTokens != 5 {
		t.Errorf("PromptTokens = %d, want 5", resp.Usage.PromptTokens)
	}

	// Verify the correct endpoint was called.
	if got := mock.Ctrl.GetLastPath(); got != "/embeddings" {
		t.Errorf("request path = %q, want %q", got, "/embeddings")
	}
}

func TestEmbed_APIError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStatusCode(http.StatusUnauthorized)
	p := New("bad-key", WithBaseURL(mock.Server.URL))

	ctx := context.Background()
	_, err := p.Embed(ctx, &provider.EmbeddingRequest{
		Model: "text-embedding-3-small",
		Input: []string{"test"},
	})
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
}

// ---------------------------------------------------------------------------
// Healthy (mock server)
// ---------------------------------------------------------------------------

func TestHealthy_True(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key", WithBaseURL(mock.Server.URL))

	if !p.Healthy(context.Background()) {
		t.Error("Healthy() = false, want true when mock returns 200")
	}

	// Verify it hits /models.
	if got := mock.Ctrl.GetLastPath(); got != "/models" {
		t.Errorf("health path = %q, want %q", got, "/models")
	}
}

func TestHealthy_False(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStatusCode(http.StatusUnauthorized)
	p := New("bad-key", WithBaseURL(mock.Server.URL))

	if p.Healthy(context.Background()) {
		t.Error("Healthy() = true, want false when mock returns 401")
	}
}

func TestHealthy_Unreachable(t *testing.T) {
	// Use a URL that will fail immediately.
	p := New("key", WithBaseURL("http://127.0.0.1:1"))

	if p.Healthy(context.Background()) {
		t.Error("Healthy() = true, want false when server is unreachable")
	}
}

// ---------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------

func TestModels(t *testing.T) {
	p := New("key")
	ctx := context.Background()
	models, err := p.Models(ctx)
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("Models() returned empty slice")
	}
}

// ---------------------------------------------------------------------------
// Conformance suite
// ---------------------------------------------------------------------------

func TestConformance(t *testing.T) {
	mock := testutil.NewMockServer(t)

	// Set up the embedding response so the conformance embed test works.
	var embResp any
	if err := json.Unmarshal(testutil.DefaultEmbeddingResponse(), &embResp); err != nil {
		t.Fatalf("unmarshal embedding response: %v", err)
	}
	mock.Ctrl.SetEmbedding(embResp)

	p := New("test-key", WithBaseURL(mock.Server.URL))
	providertest.TestProviderContract(t, p)
}

func TestConformanceComplete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key", WithBaseURL(mock.Server.URL))
	providertest.TestProviderComplete(t, p)
}

func TestConformanceEmbed(t *testing.T) {
	mock := testutil.NewMockServer(t)

	var embResp any
	if err := json.Unmarshal(testutil.DefaultEmbeddingResponse(), &embResp); err != nil {
		t.Fatalf("unmarshal embedding response: %v", err)
	}
	mock.Ctrl.SetEmbedding(embResp)

	p := New("test-key", WithBaseURL(mock.Server.URL))
	providertest.TestProviderEmbed(t, p)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func joinStrings(ss []string) string {
	out := ""
	for _, s := range ss {
		out += s
	}
	return out
}

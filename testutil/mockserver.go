// Package testutil provides shared test utilities including mock HTTP servers.
package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// MockServer is a configurable HTTP test server for provider testing.
type MockServer struct {
	Server *httptest.Server
	Ctrl   *MockController
}

// MockController configures how the mock server responds.
type MockController struct {
	mu sync.Mutex

	// Response configuration
	completionResp any
	embeddingResp  any
	streamHandler  http.HandlerFunc
	statusCode     int

	// Request capture
	LastMethod string
	LastPath   string
	LastBody   []byte
	LastHeader http.Header
}

// NewMockServer creates a new mock HTTP server for testing providers.
func NewMockServer(t *testing.T) *MockServer {
	t.Helper()

	ctrl := &MockController{
		statusCode: http.StatusOK,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctrl.mu.Lock()

		// Capture request info
		ctrl.LastMethod = r.Method
		ctrl.LastPath = r.URL.Path
		ctrl.LastHeader = r.Header.Clone()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			ctrl.mu.Unlock()
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		ctrl.LastBody = body
		_ = r.Body.Close()

		statusCode := ctrl.statusCode
		completionResp := ctrl.completionResp
		embeddingResp := ctrl.embeddingResp
		streamHandler := ctrl.streamHandler

		ctrl.mu.Unlock()

		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			if _, err := w.Write([]byte(`{"error": {"message": "mock error"}}`)); err != nil {
				return
			}
			return
		}

		// Route based on path
		switch {
		case streamHandler != nil && isStreamRequest(body):
			streamHandler(w, r)
		case embeddingResp != nil && isEmbeddingPath(r.URL.Path):
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(embeddingResp); err != nil {
				return
			}
		case completionResp != nil:
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(completionResp); err != nil {
				return
			}
		default:
			// Default: return a standard OpenAI-compatible completion response
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(DefaultCompletionResponse()); err != nil {
				return
			}
		}
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &MockServer{
		Server: server,
		Ctrl:   ctrl,
	}
}

// SetCompletion sets the completion response body.
func (c *MockController) SetCompletion(resp any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.completionResp = resp
}

// SetEmbedding sets the embedding response body.
func (c *MockController) SetEmbedding(resp any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.embeddingResp = resp
}

// SetStreamHandler sets a custom handler for streaming requests.
func (c *MockController) SetStreamHandler(h http.HandlerFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streamHandler = h
}

// SetStatusCode sets the HTTP status code to return.
func (c *MockController) SetStatusCode(code int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statusCode = code
}

// GetLastBody returns the last captured request body.
func (c *MockController) GetLastBody() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.LastBody
}

// GetLastPath returns the last captured request path.
func (c *MockController) GetLastPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.LastPath
}

// GetLastHeader returns the last captured request headers.
func (c *MockController) GetLastHeader() http.Header {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.LastHeader
}

// DefaultCompletionResponse returns a standard OpenAI-format completion response.
func DefaultCompletionResponse() []byte {
	resp := map[string]any{
		"id":      "chatcmpl-test123",
		"object":  "chat.completion",
		"created": 1700000000,
		"model":   "test-model",
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "Hello! How can I help you?",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 8,
			"total_tokens":      18,
		},
	}
	b, _ := json.Marshal(resp) //nolint:errcheck // test helper; static data cannot fail
	return b
}

// DefaultEmbeddingResponse returns a standard OpenAI-format embedding response.
func DefaultEmbeddingResponse() []byte {
	resp := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"object":    "embedding",
				"index":     0,
				"embedding": []float64{0.1, 0.2, 0.3, 0.4, 0.5},
			},
		},
		"model": "test-embed-model",
		"usage": map[string]any{
			"prompt_tokens": 5,
			"total_tokens":  5,
		},
	}
	b, _ := json.Marshal(resp) //nolint:errcheck // test helper; static data cannot fail
	return b
}

func isStreamRequest(body []byte) bool {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	stream, ok := req["stream"].(bool)
	return ok && stream
}

func isEmbeddingPath(path string) bool {
	return path == "/embeddings" || path == "/v1/embeddings"
}

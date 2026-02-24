package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/xraph/nexus/provider"
)

// handleChatCompletions handles POST /v1/chat/completions
func (p *Proxy) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var req provider.CompletionRequest
	if unmarshalErr := json.Unmarshal(body, &req); unmarshalErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+unmarshalErr.Error())
		return
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "messages is required")
		return
	}

	ctx := r.Context()

	// Streaming response
	if req.Stream {
		p.handleStreamingCompletion(w, r, &req)
		return
	}

	// Non-streaming response
	resp, err := p.engine.Complete(ctx, &req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Convert to OpenAI response format
	openAIResp := toOpenAIChatResponse(resp)
	writeJSON(w, http.StatusOK, openAIResp)
}

// handleStreamingCompletion handles streaming chat completions.
func (p *Proxy) handleStreamingCompletion(w http.ResponseWriter, r *http.Request, req *provider.CompletionRequest) {
	ctx := r.Context()
	stream, err := p.engine.CompleteStream(ctx, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer func() { _ = stream.Close() }()

	streamSSE(ctx, w, stream, req.Model)
}

// handleEmbeddings handles POST /v1/embeddings
func (p *Proxy) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Parse request — accept string or array of strings for "input"
	var raw map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(body, &raw); unmarshalErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON")
		return
	}

	var req provider.EmbeddingRequest
	if modelRaw, ok := raw["model"]; ok {
		_ = json.Unmarshal(modelRaw, &req.Model) //nolint:errcheck // best-effort; validated below
	}

	if inputRaw, ok := raw["input"]; ok {
		// Try as string first
		var single string
		if unmarshalErr := json.Unmarshal(inputRaw, &single); unmarshalErr == nil {
			req.Input = []string{single}
		} else {
			// Try as array of strings
			_ = json.Unmarshal(inputRaw, &req.Input) //nolint:errcheck // best-effort; validated below
		}
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if len(req.Input) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "input is required")
		return
	}

	resp, err := p.engine.Embed(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Convert to OpenAI embeddings response format
	openAIResp := toOpenAIEmbeddingResponse(resp)
	writeJSON(w, http.StatusOK, openAIResp)
}

// handleListModels handles GET /v1/models
func (p *Proxy) handleListModels(w http.ResponseWriter, r *http.Request) {
	models, err := p.engine.ListModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	var data []openAIModel
	for _, m := range models {
		data = append(data, openAIModel{
			ID:      m.Name,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: m.Provider,
		})
	}

	writeJSON(w, http.StatusOK, openAIModelList{
		Object: "list",
		Data:   data,
	})
}

// handleGetModel handles GET /v1/models/{model}
func (p *Proxy) handleGetModel(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("model")
	if modelID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}

	models, err := p.engine.ListModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	for _, m := range models {
		if m.Name == modelID {
			writeJSON(w, http.StatusOK, openAIModel{
				ID:      m.Name,
				Object:  "model",
				Created: time.Now().Unix(),
				OwnedBy: m.Provider,
			})
			return
		}
	}

	writeError(w, http.StatusNotFound, "model_not_found", fmt.Sprintf("model '%s' not found", modelID))
}

// handleHealth handles GET /health
func (p *Proxy) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"server": "nexus",
	})
}

// ─────────────────────────────────────────────────────────────
// OpenAI-compatible response types
// ─────────────────────────────────────────────────────────────

type openAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIMessage struct {
	Role      string              `json:"role"`
	Content   any                 `json:"content"`
	ToolCalls []provider.ToolCall `json:"tool_calls,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIModelList struct {
	Object string        `json:"object"`
	Data   []openAIModel `json:"data"`
}

type openAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type openAIEmbeddingResponse struct {
	Object string            `json:"object"`
	Data   []openAIEmbedding `json:"data"`
	Model  string            `json:"model"`
	Usage  openAIUsage       `json:"usage"`
}

type openAIEmbedding struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type openAIError struct {
	Error openAIErrorBody `json:"error"`
}

type openAIErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// ─────────────────────────────────────────────────────────────
// Conversion helpers
// ─────────────────────────────────────────────────────────────

func toOpenAIChatResponse(resp *provider.CompletionResponse) *openAIChatResponse {
	choices := make([]openAIChoice, len(resp.Choices))
	for i, c := range resp.Choices {
		choices[i] = openAIChoice{
			Index: c.Index,
			Message: openAIMessage{
				Role:      c.Message.Role,
				Content:   c.Message.Content,
				ToolCalls: c.Message.ToolCalls,
			},
			FinishReason: c.FinishReason,
		}
	}

	return &openAIChatResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: resp.Created.Unix(),
		Model:   resp.Model,
		Choices: choices,
		Usage: openAIUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
}

func toOpenAIEmbeddingResponse(resp *provider.EmbeddingResponse) *openAIEmbeddingResponse {
	data := make([]openAIEmbedding, len(resp.Embeddings))
	for i, emb := range resp.Embeddings {
		data[i] = openAIEmbedding{
			Object:    "embedding",
			Embedding: emb,
			Index:     i,
		}
	}

	return &openAIEmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  resp.Model,
		Usage: openAIUsage{
			PromptTokens: resp.Usage.PromptTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}
}

// ─────────────────────────────────────────────────────────────
// HTTP helpers
// ─────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) //nolint:errcheck // best-effort HTTP response write
}

func writeError(w http.ResponseWriter, status int, errType, message string) {
	writeJSON(w, status, openAIError{
		Error: openAIErrorBody{
			Message: message,
			Type:    errType,
		},
	})
}

package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/xraph/nexus/provider"
)

type client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

func newClient(apiKey, baseURL string) *client {
	return &client{
		apiKey:  apiKey,
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Gemini request types.
type geminiRequest struct {
	Contents          []geminiContent    `json:"contents"`
	SystemInstruction *geminiContent     `json:"systemInstruction,omitempty"`
	GenerationConfig  *generationConfig  `json:"generationConfig,omitempty"`
	Tools             []geminiToolConfig `json:"tools,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text         string            `json:"text,omitempty"`
	FunctionCall *geminiFuncCall   `json:"functionCall,omitempty"`
	FunctionResp *geminiFuncResult `json:"functionResponse,omitempty"`
}

type geminiFuncCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFuncResult struct {
	Name     string `json:"name"`
	Response any    `json:"response"`
}

type generationConfig struct {
	MaxOutputTokens  int      `json:"maxOutputTokens,omitempty"`
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"topP,omitempty"`
	StopSequences    []string `json:"stopSequences,omitempty"`
	ResponseMimeType string   `json:"responseMimeType,omitempty"`
}

type geminiToolConfig struct {
	FunctionDeclarations []geminiFuncDecl `json:"functionDeclarations,omitempty"`
}

type geminiFuncDecl struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// Gemini response types.
type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata *geminiUsage      `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// Embedding types.
type geminiEmbedRequest struct {
	Model   string             `json:"model"`
	Content geminiEmbedContent `json:"content"`
}

type geminiEmbedContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiBatchEmbedRequest struct {
	Requests []geminiEmbedRequest `json:"requests"`
}

type geminiBatchEmbedResponse struct {
	Embeddings []struct {
		Values []float64 `json:"values"`
	} `json:"embeddings"`
}

func (c *client) complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	gemReq := c.toGeminiRequest(req)

	body, err := json.Marshal(gemReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", c.baseURL, req.Model, c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	elapsed := time.Since(start)

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("gemini: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var gemResp geminiResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&gemResp); err != nil {
		return nil, fmt.Errorf("gemini: decode response: %w", err)
	}

	return c.fromGeminiResponse(&gemResp, req.Model, elapsed), nil
}

func (c *client) completeStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	gemReq := c.toGeminiRequest(req)

	body, err := json.Marshal(gemReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", c.baseURL, req.Model, c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer func() { _ = httpResp.Body.Close() }()
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("gemini: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	return newGeminiStream(httpResp.Body, req.Model), nil
}

func (c *client) embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	// Use batch embedding endpoint.
	requests := make([]geminiEmbedRequest, len(req.Input))
	for i, text := range req.Input {
		requests[i] = geminiEmbedRequest{
			Model: fmt.Sprintf("models/%s", req.Model),
			Content: geminiEmbedContent{
				Parts: []geminiPart{{Text: text}},
			},
		}
	}

	batchReq := geminiBatchEmbedRequest{Requests: requests}
	body, err := json.Marshal(batchReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal embed request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:batchEmbedContents?key=%s", c.baseURL, req.Model, c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: create embed request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: embed request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("gemini: embed API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var embedResp geminiBatchEmbedResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("gemini: decode embed response: %w", err)
	}

	embeddings := make([][]float64, len(embedResp.Embeddings))
	for i, e := range embedResp.Embeddings {
		embeddings[i] = e.Values
	}

	return &provider.EmbeddingResponse{
		Provider:   "gemini",
		Model:      req.Model,
		Embeddings: embeddings,
		Usage: provider.Usage{
			PromptTokens: len(req.Input), // Gemini doesn't return token counts for embeddings
			TotalTokens:  len(req.Input),
		},
	}, nil
}

func (c *client) ping(ctx context.Context) error {
	url := fmt.Sprintf("%s/v1beta/models?key=%s", c.baseURL, c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	_, _ = io.ReadAll(httpResp.Body)

	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("gemini: health check failed with status %d", httpResp.StatusCode)
	}
	return nil
}

func (c *client) toGeminiRequest(req *provider.CompletionRequest) *geminiRequest {
	gemReq := &geminiRequest{}

	// Handle system message.
	if req.System != "" {
		gemReq.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	// Convert messages.
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			// Combine system messages into system instruction.
			if text, ok := m.Content.(string); ok {
				if gemReq.SystemInstruction == nil {
					gemReq.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: text}}}
				} else {
					gemReq.SystemInstruction.Parts = append(gemReq.SystemInstruction.Parts, geminiPart{Text: text})
				}
			}
		case "user":
			parts := contentToParts(m.Content)
			gemReq.Contents = append(gemReq.Contents, geminiContent{
				Role:  "user",
				Parts: parts,
			})
		case "assistant":
			parts := contentToParts(m.Content)
			// Include tool calls as function call parts.
			for _, tc := range m.ToolCalls {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFuncCall{
						Name: tc.Function.Name,
						Args: args,
					},
				})
			}
			gemReq.Contents = append(gemReq.Contents, geminiContent{
				Role:  "model",
				Parts: parts,
			})
		case "tool":
			// Tool response.
			var resp any
			_ = json.Unmarshal([]byte(fmt.Sprintf("%v", m.Content)), &resp)
			gemReq.Contents = append(gemReq.Contents, geminiContent{
				Role: "user",
				Parts: []geminiPart{{
					FunctionResp: &geminiFuncResult{
						Name:     m.Name,
						Response: resp,
					},
				}},
			})
		}
	}

	// Generation config.
	config := &generationConfig{
		MaxOutputTokens: req.MaxTokens,
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		StopSequences:   req.Stop,
	}
	if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object" {
		config.ResponseMimeType = "application/json"
	}
	gemReq.GenerationConfig = config

	// Tools.
	if len(req.Tools) > 0 {
		var decls []geminiFuncDecl
		for _, t := range req.Tools {
			decls = append(decls, geminiFuncDecl{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			})
		}
		gemReq.Tools = []geminiToolConfig{{FunctionDeclarations: decls}}
	}

	return gemReq
}

func contentToParts(content any) []geminiPart {
	switch v := content.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []geminiPart{{Text: v}}
	case []any:
		var parts []geminiPart
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				switch m["type"] {
				case "text":
					if text, ok := m["text"].(string); ok {
						parts = append(parts, geminiPart{Text: text})
					}
				}
			}
		}
		return parts
	default:
		return nil
	}
}

func (c *client) fromGeminiResponse(resp *geminiResponse, model string, elapsed time.Duration) *provider.CompletionResponse {
	var choices []provider.Choice

	for i, candidate := range resp.Candidates {
		var content string
		var toolCalls []provider.ToolCall

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				content += part.Text
			}
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, provider.ToolCall{
					ID:   fmt.Sprintf("call_%d", len(toolCalls)),
					Type: "function",
					Function: provider.ToolCallFunc{
						Name:      part.FunctionCall.Name,
						Arguments: string(argsJSON),
					},
				})
			}
		}

		choices = append(choices, provider.Choice{
			Index: i,
			Message: provider.Message{
				Role:      "assistant",
				Content:   content,
				ToolCalls: toolCalls,
			},
			FinishReason: mapFinishReason(candidate.FinishReason),
		})
	}

	result := &provider.CompletionResponse{
		ID:       fmt.Sprintf("gemini-%d", time.Now().UnixNano()),
		Provider: "gemini",
		Model:    model,
		Created:  time.Now(),
		Choices:  choices,
		Latency:  elapsed,
	}

	if resp.UsageMetadata != nil {
		result.Usage = provider.Usage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		}
	}

	return result
}

func mapFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	case "RECITATION":
		return "content_filter"
	default:
		return reason
	}
}

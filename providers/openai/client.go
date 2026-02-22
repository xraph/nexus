package openai

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
	orgID   string
	http    *http.Client
}

func newClient(apiKey, baseURL, orgID string) *client {
	return &client{
		apiKey:  apiKey,
		baseURL: baseURL,
		orgID:   orgID,
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// openAIRequest is the OpenAI chat completion request format.
type openAIRequest struct {
	Model          string                   `json:"model"`
	Messages       []openAIMessage          `json:"messages"`
	MaxTokens      int                      `json:"max_tokens,omitempty"`
	Temperature    *float64                 `json:"temperature,omitempty"`
	TopP           *float64                 `json:"top_p,omitempty"`
	Stop           []string                 `json:"stop,omitempty"`
	Stream         bool                     `json:"stream,omitempty"`
	Tools          []provider.Tool          `json:"tools,omitempty"`
	ToolChoice     any                      `json:"tool_choice,omitempty"`
	ResponseFormat *provider.ResponseFormat `json:"response_format,omitempty"`
}

type openAIMessage struct {
	Role       string              `json:"role"`
	Content    any                 `json:"content"`
	Name       string              `json:"name,omitempty"`
	ToolCalls  []provider.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int           `json:"index"`
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *client) complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	oaiReq := c.toOpenAIRequest(req)
	oaiReq.Stream = false

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	c.setHeaders(httpReq)

	start := time.Now()
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}
	defer httpResp.Body.Close()
	elapsed := time.Since(start)

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("openai: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var oaiResp openAIResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}

	return c.fromOpenAIResponse(&oaiResp, elapsed), nil
}

func (c *client) completeStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	oaiReq := c.toOpenAIRequest(req)
	oaiReq.Stream = true

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	c.setHeaders(httpReq)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("openai: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	return newOpenAIStream(httpResp.Body, req.Model), nil
}

func (c *client) embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	payload := map[string]any{
		"model": req.Model,
		"input": req.Input,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	c.setHeaders(httpReq)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("openai: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var resp struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}

	embeddings := make([][]float64, len(resp.Data))
	for i, d := range resp.Data {
		embeddings[i] = d.Embedding
	}

	return &provider.EmbeddingResponse{
		Provider:   "openai",
		Model:      req.Model,
		Embeddings: embeddings,
		Usage: provider.Usage{
			PromptTokens: resp.Usage.PromptTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}, nil
}

func (c *client) ping(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	c.setHeaders(httpReq)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	_, _ = io.ReadAll(httpResp.Body)

	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("openai: health check failed with status %d", httpResp.StatusCode)
	}
	return nil
}

func (c *client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if c.orgID != "" {
		req.Header.Set("OpenAI-Organization", c.orgID)
	}
}

func (c *client) toOpenAIRequest(req *provider.CompletionRequest) *openAIRequest {
	messages := make([]openAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = openAIMessage{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
	}

	// Prepend system message if set via Anthropic-style System field
	if req.System != "" {
		messages = append([]openAIMessage{{
			Role:    "system",
			Content: req.System,
		}}, messages...)
	}

	return &openAIRequest{
		Model:          req.Model,
		Messages:       messages,
		MaxTokens:      req.MaxTokens,
		Temperature:    req.Temperature,
		TopP:           req.TopP,
		Stop:           req.Stop,
		Tools:          req.Tools,
		ToolChoice:     req.ToolChoice,
		ResponseFormat: req.ResponseFormat,
	}
}

func (c *client) fromOpenAIResponse(resp *openAIResponse, elapsed time.Duration) *provider.CompletionResponse {
	choices := make([]provider.Choice, len(resp.Choices))
	for i, ch := range resp.Choices {
		choices[i] = provider.Choice{
			Index: ch.Index,
			Message: provider.Message{
				Role:      ch.Message.Role,
				Content:   ch.Message.Content,
				ToolCalls: ch.Message.ToolCalls,
			},
			FinishReason: ch.FinishReason,
		}
	}

	return &provider.CompletionResponse{
		ID:       resp.ID,
		Provider: "openai",
		Model:    resp.Model,
		Created:  time.Unix(resp.Created, 0),
		Choices:  choices,
		Usage: provider.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
		Latency: elapsed,
	}
}

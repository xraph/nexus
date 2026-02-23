package azureopenai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/nexus/provider"
)

type client struct {
	apiKey       string
	resourceName string
	deploymentID string
	apiVersion   string
	baseURL      string
	http         *http.Client
}

func newClient(apiKey, resourceName, deploymentID, apiVersion, baseURL string) *client {
	if baseURL == "" && resourceName != "" {
		baseURL = fmt.Sprintf("https://%s.openai.azure.com", resourceName)
	}
	return &client{
		apiKey:       apiKey,
		resourceName: resourceName,
		deploymentID: deploymentID,
		apiVersion:   apiVersion,
		baseURL:      baseURL,
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// OpenAI-compatible request types (same wire format).

type oaiRequest struct {
	Model          string                   `json:"model"`
	Messages       []oaiMessage             `json:"messages"`
	MaxTokens      int                      `json:"max_tokens,omitempty"`
	Temperature    *float64                 `json:"temperature,omitempty"`
	TopP           *float64                 `json:"top_p,omitempty"`
	Stop           []string                 `json:"stop,omitempty"`
	Stream         bool                     `json:"stream,omitempty"`
	Tools          []provider.Tool          `json:"tools,omitempty"`
	ToolChoice     any                      `json:"tool_choice,omitempty"`
	ResponseFormat *provider.ResponseFormat `json:"response_format,omitempty"`
}

type oaiMessage struct {
	Role       string              `json:"role"`
	Content    any                 `json:"content"`
	Name       string              `json:"name,omitempty"`
	ToolCalls  []provider.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
}

type oaiResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int        `json:"index"`
		Message      oaiMessage `json:"message"`
		FinishReason string     `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *client) chatCompletionsURL() string {
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		c.baseURL, c.deploymentID, c.apiVersion)
}

func (c *client) embeddingsURL() string {
	return fmt.Sprintf("%s/openai/deployments/%s/embeddings?api-version=%s",
		c.baseURL, c.deploymentID, c.apiVersion)
}

func (c *client) complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	oaiReq := c.toOAIRequest(req)
	oaiReq.Stream = false

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("azureopenai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.chatCompletionsURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("azureopenai: create request: %w", err)
	}
	c.setHeaders(httpReq)

	start := time.Now()
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("azureopenai: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	elapsed := time.Since(start)

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("azureopenai: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var oaiResp oaiResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("azureopenai: decode response: %w", err)
	}

	return c.fromOAIResponse(&oaiResp, elapsed), nil
}

func (c *client) completeStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	oaiReq := c.toOAIRequest(req)
	oaiReq.Stream = true

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("azureopenai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.chatCompletionsURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("azureopenai: create request: %w", err)
	}
	c.setHeaders(httpReq)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("azureopenai: request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer func() { _ = httpResp.Body.Close() }()
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("azureopenai: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	return newAzureStream(httpResp.Body, req.Model), nil
}

func (c *client) embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	payload := map[string]any{
		"model": req.Model,
		"input": req.Input,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("azureopenai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.embeddingsURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("azureopenai: create request: %w", err)
	}
	c.setHeaders(httpReq)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("azureopenai: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("azureopenai: API error (status %d): %s", httpResp.StatusCode, string(respBody))
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
		return nil, fmt.Errorf("azureopenai: decode response: %w", err)
	}

	embeddings := make([][]float64, len(resp.Data))
	for i, d := range resp.Data {
		embeddings[i] = d.Embedding
	}

	return &provider.EmbeddingResponse{
		Provider:   "azureopenai",
		Model:      req.Model,
		Embeddings: embeddings,
		Usage: provider.Usage{
			PromptTokens: resp.Usage.PromptTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}, nil
}

func (c *client) ping(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.chatCompletionsURL(), nil)
	if err != nil {
		return err
	}
	c.setHeaders(httpReq)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	_, _ = io.ReadAll(httpResp.Body)

	// Azure returns various non-5xx codes for valid endpoints; treat 5xx as unhealthy.
	if httpResp.StatusCode >= 500 {
		return fmt.Errorf("azureopenai: health check failed with status %d", httpResp.StatusCode)
	}
	return nil
}

func (c *client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", c.apiKey)
}

func (c *client) toOAIRequest(req *provider.CompletionRequest) *oaiRequest {
	messages := make([]oaiMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = oaiMessage{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
	}

	// Prepend system message if set via Anthropic-style System field.
	if req.System != "" {
		messages = append([]oaiMessage{{
			Role:    "system",
			Content: req.System,
		}}, messages...)
	}

	return &oaiRequest{
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

func (c *client) fromOAIResponse(resp *oaiResponse, elapsed time.Duration) *provider.CompletionResponse {
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
		Provider: "azureopenai",
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

// azureStream implements provider.Stream for Azure OpenAI SSE responses.
type azureStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	model   string
	usage   *provider.Usage
	done    bool
}

func newAzureStream(body io.ReadCloser, model string) *azureStream {
	return &azureStream{
		body:    body,
		scanner: bufio.NewScanner(body),
		model:   model,
	}
}

func (s *azureStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s.done {
		return nil, io.EOF
	}

	for s.scanner.Scan() {
		line := s.scanner.Text()

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// SSE format: "data: {...}"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Check for stream end.
		if data == "[DONE]" {
			s.done = true
			return nil, io.EOF
		}

		var chunk struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Role      string              `json:"role,omitempty"`
					Content   string              `json:"content,omitempty"`
					ToolCalls []provider.ToolCall `json:"tool_calls,omitempty"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage,omitempty"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, fmt.Errorf("azureopenai: decode stream chunk: %w", err)
		}

		// Capture usage if present.
		if chunk.Usage != nil {
			s.usage = &provider.Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		ch := chunk.Choices[0]
		finishReason := ""
		if ch.FinishReason != nil {
			finishReason = *ch.FinishReason
		}

		return &provider.StreamChunk{
			ID:       chunk.ID,
			Provider: "azureopenai",
			Model:    chunk.Model,
			Delta: provider.Delta{
				Role:      ch.Delta.Role,
				Content:   ch.Delta.Content,
				ToolCalls: ch.Delta.ToolCalls,
			},
			FinishReason: finishReason,
		}, nil
	}

	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("azureopenai: stream read error: %w", err)
	}

	s.done = true
	return nil, io.EOF
}

func (s *azureStream) Close() error {
	s.done = true
	return s.body.Close()
}

func (s *azureStream) Usage() *provider.Usage {
	return s.usage
}

// Compile-time check.
var _ provider.Stream = (*azureStream)(nil)

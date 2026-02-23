package cohere

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

// Cohere v2 chat request format.
type cohereRequest struct {
	Model          string          `json:"model"`
	Messages       []cohereMessage `json:"messages"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	TopP           *float64        `json:"p,omitempty"`
	Stop           []string        `json:"stop_sequences,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
	Tools          []cohereTool    `json:"tools,omitempty"`
	ResponseFormat *cohereRespFmt  `json:"response_format,omitempty"`
}

type cohereMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type cohereTool struct {
	Type     string             `json:"type"`
	Function cohereToolFunction `json:"function"`
}

type cohereToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type cohereRespFmt struct {
	Type string `json:"type"`
}

// Cohere v2 chat response format.
type cohereResponse struct {
	ID           string            `json:"id"`
	Message      cohereRespMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
	Usage        cohereUsage       `json:"usage"`
}

type cohereRespMessage struct {
	Role      string               `json:"role"`
	Content   []cohereContentBlock `json:"content"`
	ToolCalls []cohereToolCall     `json:"tool_calls,omitempty"`
}

type cohereContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type cohereToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type cohereUsage struct {
	BilledUnits struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"billed_units"`
	Tokens struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"tokens"`
}

// Embed types.
type cohereEmbedRequest struct {
	Model     string   `json:"model"`
	Texts     []string `json:"texts"`
	InputType string   `json:"input_type"`
}

type cohereEmbedResponse struct {
	ID         string      `json:"id"`
	Embeddings [][]float64 `json:"embeddings"`
	Meta       struct {
		BilledUnits struct {
			InputTokens int `json:"input_tokens"`
		} `json:"billed_units"`
	} `json:"meta"`
}

func (c *client) complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	cohReq := c.toCohereRequest(req)
	cohReq.Stream = false

	body, err := json.Marshal(cohReq)
	if err != nil {
		return nil, fmt.Errorf("cohere: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v2/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cohere: create request: %w", err)
	}
	c.setHeaders(httpReq)

	start := time.Now()
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("cohere: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	elapsed := time.Since(start)

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("cohere: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var cohResp cohereResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&cohResp); err != nil {
		return nil, fmt.Errorf("cohere: decode response: %w", err)
	}

	return c.fromCohereResponse(&cohResp, elapsed), nil
}

func (c *client) completeStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	cohReq := c.toCohereRequest(req)
	cohReq.Stream = true

	body, err := json.Marshal(cohReq)
	if err != nil {
		return nil, fmt.Errorf("cohere: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v2/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cohere: create request: %w", err)
	}
	c.setHeaders(httpReq)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("cohere: request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer func() { _ = httpResp.Body.Close() }()
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("cohere: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	return newCohereStream(httpResp.Body, req.Model), nil
}

func (c *client) embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	embedReq := cohereEmbedRequest{
		Model:     req.Model,
		Texts:     req.Input,
		InputType: "search_document",
	}

	body, err := json.Marshal(embedReq)
	if err != nil {
		return nil, fmt.Errorf("cohere: marshal embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v2/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cohere: create embed request: %w", err)
	}
	c.setHeaders(httpReq)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("cohere: embed request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("cohere: embed API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var embedResp cohereEmbedResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("cohere: decode embed response: %w", err)
	}

	return &provider.EmbeddingResponse{
		Provider:   "cohere",
		Model:      req.Model,
		Embeddings: embedResp.Embeddings,
		Usage: provider.Usage{
			PromptTokens: embedResp.Meta.BilledUnits.InputTokens,
			TotalTokens:  embedResp.Meta.BilledUnits.InputTokens,
		},
	}, nil
}

func (c *client) ping(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/v2/models", nil)
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

	if httpResp.StatusCode != http.StatusOK && httpResp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("cohere: health check failed with status %d", httpResp.StatusCode)
	}
	return nil
}

func (c *client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
}

func (c *client) toCohereRequest(req *provider.CompletionRequest) *cohereRequest {
	var messages []cohereMessage

	// System message.
	if req.System != "" {
		messages = append(messages, cohereMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	for _, m := range req.Messages {
		content := ""
		if s, ok := m.Content.(string); ok {
			content = s
		}
		msg := cohereMessage{
			Role:       m.Role,
			Content:    content,
			ToolCallID: m.ToolCallID,
		}
		messages = append(messages, msg)
	}

	cohReq := &cohereRequest{
		Model:       req.Model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.Stop,
	}

	// Tools.
	for _, t := range req.Tools {
		cohReq.Tools = append(cohReq.Tools, cohereTool{
			Type: "function",
			Function: cohereToolFunction{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		})
	}

	// Response format.
	if req.ResponseFormat != nil {
		cohReq.ResponseFormat = &cohereRespFmt{Type: req.ResponseFormat.Type}
	}

	return cohReq
}

func (c *client) fromCohereResponse(resp *cohereResponse, elapsed time.Duration) *provider.CompletionResponse {
	var content string
	for _, block := range resp.Message.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	var toolCalls []provider.ToolCall
	for _, tc := range resp.Message.ToolCalls {
		toolCalls = append(toolCalls, provider.ToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: provider.ToolCallFunc{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	inputTokens := resp.Usage.Tokens.InputTokens
	outputTokens := resp.Usage.Tokens.OutputTokens

	return &provider.CompletionResponse{
		ID:       resp.ID,
		Provider: "cohere",
		Model:    "",
		Created:  time.Now(),
		Choices: []provider.Choice{{
			Index: 0,
			Message: provider.Message{
				Role:      "assistant",
				Content:   content,
				ToolCalls: toolCalls,
			},
			FinishReason: mapCohereFinishReason(resp.FinishReason),
		}},
		Usage: provider.Usage{
			PromptTokens:     inputTokens,
			CompletionTokens: outputTokens,
			TotalTokens:      inputTokens + outputTokens,
		},
		Latency: elapsed,
	}
}

func mapCohereFinishReason(reason string) string {
	switch reason {
	case "COMPLETE":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "TOOL_CALL":
		return "tool_calls"
	default:
		return reason
	}
}

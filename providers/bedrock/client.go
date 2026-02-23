package bedrock

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
	signer  *sigV4Signer
	region  string
	baseURL string
	http    *http.Client
}

func newClient(accessKeyID, secretAccessKey, sessionToken, region, baseURL string) *client {
	signer := newSigV4Signer(accessKeyID, secretAccessKey, region)
	signer.sessionToken = sessionToken

	if baseURL == "" {
		baseURL = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region)
	}

	return &client{
		signer:  signer,
		region:  region,
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Bedrock Converse API request types.

type converseRequest struct {
	Messages        []converseMessage `json:"messages"`
	System          []systemContent   `json:"system,omitempty"`
	InferenceConfig *inferenceConfig  `json:"inferenceConfig,omitempty"`
	ToolConfig      *toolConfig       `json:"toolConfig,omitempty"`
}

type converseMessage struct {
	Role    string           `json:"role"`
	Content []contentBlock   `json:"content"`
}

type contentBlock struct {
	Text    string   `json:"text,omitempty"`
	ToolUse *toolUse `json:"toolUse,omitempty"`
	ToolResult *toolResult `json:"toolResult,omitempty"`
}

type toolUse struct {
	ToolUseID string `json:"toolUseId"`
	Name      string `json:"name"`
	Input     any    `json:"input"`
}

type toolResult struct {
	ToolUseID string          `json:"toolUseId"`
	Content   []contentBlock  `json:"content"`
}

type systemContent struct {
	Text string `json:"text"`
}

type inferenceConfig struct {
	MaxTokens     int      `json:"maxTokens,omitempty"`
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"topP,omitempty"`
	StopSequences []string `json:"stopSequences,omitempty"`
}

type toolConfig struct {
	Tools []toolDef `json:"tools"`
}

type toolDef struct {
	ToolSpec *toolSpec `json:"toolSpec"`
}

type toolSpec struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	JSON any `json:"json"`
}

// Bedrock Converse API response types.

type converseResponse struct {
	Output     converseOutput `json:"output"`
	StopReason string         `json:"stopReason"`
	Usage      converseUsage  `json:"usage"`
}

type converseOutput struct {
	Message *converseMessage `json:"message,omitempty"`
}

type converseUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

func (c *client) complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	convReq := c.toConverseRequest(req)

	body, err := json.Marshal(convReq)
	if err != nil {
		return nil, fmt.Errorf("bedrock: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/model/%s/converse", c.baseURL, req.Model)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bedrock: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	if err := c.signer.Sign(httpReq, body, time.Now()); err != nil {
		return nil, fmt.Errorf("bedrock: sign request: %w", err)
	}

	start := time.Now()
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bedrock: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	elapsed := time.Since(start)

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("bedrock: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var convResp converseResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&convResp); err != nil {
		return nil, fmt.Errorf("bedrock: decode response: %w", err)
	}

	return c.fromConverseResponse(&convResp, req.Model, elapsed), nil
}

func (c *client) completeStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	convReq := c.toConverseRequest(req)

	body, err := json.Marshal(convReq)
	if err != nil {
		return nil, fmt.Errorf("bedrock: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/model/%s/converse-with-response-stream", c.baseURL, req.Model)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bedrock: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/vnd.amazon.eventstream")

	if err := c.signer.Sign(httpReq, body, time.Now()); err != nil {
		return nil, fmt.Errorf("bedrock: sign request: %w", err)
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("bedrock: request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer func() { _ = httpResp.Body.Close() }()
		respBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("bedrock: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	return newBedrockStream(httpResp.Body, req.Model), nil
}

func (c *client) ping(ctx context.Context) error {
	// Use a lightweight request to check if the Bedrock endpoint is reachable.
	// A GET to the base URL will return an error response, but connectivity is confirmed
	// if we get any HTTP response back.
	httpReq, err := http.NewRequestWithContext(ctx, "GET", c.baseURL, nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.signer.Sign(httpReq, nil, time.Now()); err != nil {
		return fmt.Errorf("bedrock: sign ping: %w", err)
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	_, _ = io.ReadAll(httpResp.Body)

	// Any response (even 403/404) means the endpoint is reachable.
	// Only network errors indicate the provider is unhealthy.
	return nil
}

func (c *client) toConverseRequest(req *provider.CompletionRequest) *converseRequest {
	convReq := &converseRequest{}

	// Handle system prompt.
	system := req.System
	for _, m := range req.Messages {
		if m.Role == "system" {
			if s, ok := m.Content.(string); ok {
				if system == "" {
					system = s
				} else {
					system += "\n\n" + s
				}
			}
		}
	}
	if system != "" {
		convReq.System = []systemContent{{Text: system}}
	}

	// Convert messages (skip system messages, they were extracted above).
	for _, m := range req.Messages {
		if m.Role == "system" {
			continue
		}

		switch m.Role {
		case "user":
			blocks := textToContentBlocks(m.Content)
			convReq.Messages = append(convReq.Messages, converseMessage{
				Role:    "user",
				Content: blocks,
			})
		case "assistant":
			blocks := textToContentBlocks(m.Content)
			// Include tool calls as toolUse content blocks.
			for _, tc := range m.ToolCalls {
				var input any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
				blocks = append(blocks, contentBlock{
					ToolUse: &toolUse{
						ToolUseID: tc.ID,
						Name:      tc.Function.Name,
						Input:     input,
					},
				})
			}
			convReq.Messages = append(convReq.Messages, converseMessage{
				Role:    "assistant",
				Content: blocks,
			})
		case "tool":
			// Tool result message. Bedrock expects this as a user message with toolResult.
			var resultContent []contentBlock
			if s, ok := m.Content.(string); ok && s != "" {
				resultContent = []contentBlock{{Text: s}}
			}
			convReq.Messages = append(convReq.Messages, converseMessage{
				Role: "user",
				Content: []contentBlock{{
					ToolResult: &toolResult{
						ToolUseID: m.ToolCallID,
						Content:   resultContent,
					},
				}},
			})
		}
	}

	// Inference configuration.
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	convReq.InferenceConfig = &inferenceConfig{
		MaxTokens:     maxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: req.Stop,
	}

	// Tool configuration.
	if len(req.Tools) > 0 {
		var tools []toolDef
		for _, t := range req.Tools {
			tools = append(tools, toolDef{
				ToolSpec: &toolSpec{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					InputSchema: inputSchema{JSON: t.Function.Parameters},
				},
			})
		}
		convReq.ToolConfig = &toolConfig{Tools: tools}
	}

	return convReq
}

// textToContentBlocks converts a message content value (string or multipart)
// into Bedrock content blocks.
func textToContentBlocks(content any) []contentBlock {
	switch v := content.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []contentBlock{{Text: v}}
	case []any:
		var blocks []contentBlock
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if m["type"] == "text" {
					if text, ok := m["text"].(string); ok {
						blocks = append(blocks, contentBlock{Text: text})
					}
				}
			}
		}
		return blocks
	default:
		return nil
	}
}

func (c *client) fromConverseResponse(resp *converseResponse, model string, elapsed time.Duration) *provider.CompletionResponse {
	var content string
	var toolCalls []provider.ToolCall

	if resp.Output.Message != nil {
		for _, block := range resp.Output.Message.Content {
			if block.Text != "" {
				content += block.Text
			}
			if block.ToolUse != nil {
				inputJSON, _ := json.Marshal(block.ToolUse.Input)
				toolCalls = append(toolCalls, provider.ToolCall{
					ID:   block.ToolUse.ToolUseID,
					Type: "function",
					Function: provider.ToolCallFunc{
						Name:      block.ToolUse.Name,
						Arguments: string(inputJSON),
					},
				})
			}
		}
	}

	return &provider.CompletionResponse{
		ID:       fmt.Sprintf("bedrock-%d", time.Now().UnixNano()),
		Provider: "bedrock",
		Model:    model,
		Created:  time.Now(),
		Choices: []provider.Choice{{
			Index: 0,
			Message: provider.Message{
				Role:      "assistant",
				Content:   content,
				ToolCalls: toolCalls,
			},
			FinishReason: mapStopReason(resp.StopReason),
		}},
		Usage: provider.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
		Latency: elapsed,
	}
}

func mapStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "stop_sequence":
		return "stop"
	default:
		return reason
	}
}

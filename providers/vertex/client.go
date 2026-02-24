package vertex

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
	projectID   string
	location    string
	baseURL     string
	tokenSource *tokenSource
	http        *http.Client
}

func newClient(projectID, location, accessToken string, credentialsJSON []byte, baseURL string) *client {
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s-aiplatform.googleapis.com", location)
	}

	ts, _ := newTokenSource(accessToken, credentialsJSON) //nolint:errcheck // optional auth

	return &client{
		projectID:   projectID,
		location:    location,
		baseURL:     baseURL,
		tokenSource: ts,
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Gemini-compatible request types.

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

// Gemini-compatible response types.

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

type vertexEmbedRequest struct {
	Instances []vertexEmbedInstance `json:"instances"`
}

type vertexEmbedInstance struct {
	Content string `json:"content"`
}

type vertexEmbedResponse struct {
	Predictions []struct {
		Embeddings struct {
			Values []float64 `json:"values"`
		} `json:"embeddings"`
	} `json:"predictions"`
}

func (c *client) generateContentURL(model string) string {
	return fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		c.baseURL, c.projectID, c.location, model)
}

func (c *client) streamGenerateContentURL(model string) string {
	return fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:streamGenerateContent?alt=sse",
		c.baseURL, c.projectID, c.location, model)
}

func (c *client) predictURL(model string) string {
	return fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:predict",
		c.baseURL, c.projectID, c.location, model)
}

func (c *client) complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	gemReq := c.toGeminiRequest(req)

	body, err := json.Marshal(gemReq)
	if err != nil {
		return nil, fmt.Errorf("vertex: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.generateContentURL(req.Model), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("vertex: create request: %w", err)
	}
	if setErr := c.setHeaders(httpReq); setErr != nil {
		return nil, fmt.Errorf("vertex: set headers: %w", setErr)
	}

	start := time.Now()
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vertex: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	elapsed := time.Since(start)

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body) //nolint:errcheck // best-effort read for error message
		return nil, fmt.Errorf("vertex: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var gemResp geminiResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&gemResp); err != nil {
		return nil, fmt.Errorf("vertex: decode response: %w", err)
	}

	return c.fromGeminiResponse(&gemResp, req.Model, elapsed), nil
}

func (c *client) completeStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	gemReq := c.toGeminiRequest(req)

	body, err := json.Marshal(gemReq)
	if err != nil {
		return nil, fmt.Errorf("vertex: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.streamGenerateContentURL(req.Model), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("vertex: create request: %w", err)
	}
	if setErr := c.setHeaders(httpReq); setErr != nil {
		return nil, fmt.Errorf("vertex: set headers: %w", setErr)
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vertex: request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer func() { _ = httpResp.Body.Close() }()
		respBody, _ := io.ReadAll(httpResp.Body) //nolint:errcheck // best-effort read for error message
		return nil, fmt.Errorf("vertex: API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	return newVertexStream(httpResp.Body, req.Model), nil
}

func (c *client) embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	instances := make([]vertexEmbedInstance, len(req.Input))
	for i, text := range req.Input {
		instances[i] = vertexEmbedInstance{Content: text}
	}

	embedReq := vertexEmbedRequest{Instances: instances}
	body, err := json.Marshal(embedReq)
	if err != nil {
		return nil, fmt.Errorf("vertex: marshal embed request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.predictURL(req.Model), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("vertex: create embed request: %w", err)
	}
	if setErr := c.setHeaders(httpReq); setErr != nil {
		return nil, fmt.Errorf("vertex: set headers: %w", setErr)
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vertex: embed request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body) //nolint:errcheck // best-effort read for error message
		return nil, fmt.Errorf("vertex: embed API error (status %d): %s", httpResp.StatusCode, string(respBody))
	}

	var embedResp vertexEmbedResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("vertex: decode embed response: %w", err)
	}

	embeddings := make([][]float64, len(embedResp.Predictions))
	for i, p := range embedResp.Predictions {
		embeddings[i] = p.Embeddings.Values
	}

	return &provider.EmbeddingResponse{
		Provider:   "vertex",
		Model:      req.Model,
		Embeddings: embeddings,
		Usage: provider.Usage{
			PromptTokens: len(req.Input),
			TotalTokens:  len(req.Input),
		},
	}, nil
}

func (c *client) ping(ctx context.Context) error {
	url := fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models",
		c.baseURL, c.projectID, c.location)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return err
	}
	if setErr := c.setHeaders(httpReq); setErr != nil {
		return fmt.Errorf("vertex: set headers: %w", setErr)
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	_, _ = io.ReadAll(httpResp.Body) //nolint:errcheck // drain body before close

	if httpResp.StatusCode >= 500 {
		return fmt.Errorf("vertex: health check failed with status %d", httpResp.StatusCode)
	}
	return nil
}

func (c *client) setHeaders(req *http.Request) error {
	req.Header.Set("Content-Type", "application/json")

	if c.tokenSource != nil {
		token, err := c.tokenSource.Token()
		if err != nil {
			return fmt.Errorf("vertex: get access token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
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
			for _, tc := range m.ToolCalls {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args) //nolint:errcheck // best-effort parse
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
			var resp any
			_ = json.Unmarshal([]byte(fmt.Sprintf("%v", m.Content)), &resp) //nolint:errcheck // best-effort parse
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
		decls := make([]geminiFuncDecl, 0, len(req.Tools))
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
				if m["type"] == "text" {
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
	choices := make([]provider.Choice, 0, len(resp.Candidates))

	for i, candidate := range resp.Candidates {
		var content string
		var toolCalls []provider.ToolCall

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				content += part.Text
			}
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args) //nolint:errcheck // known-good struct
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
		ID:       fmt.Sprintf("vertex-%d", time.Now().UnixNano()),
		Provider: "vertex",
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

// vertexStream implements provider.Stream for Vertex AI SSE responses.
type vertexStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	model   string
	usage   *provider.Usage
	done    bool
}

func newVertexStream(body io.ReadCloser, model string) *vertexStream {
	return &vertexStream{
		body:    body,
		scanner: bufio.NewScanner(body),
		model:   model,
	}
}

func (s *vertexStream) Next(_ context.Context) (*provider.StreamChunk, error) {
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

		var resp geminiResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			return nil, fmt.Errorf("vertex: decode stream chunk: %w", err)
		}

		// Capture usage if present.
		if resp.UsageMetadata != nil {
			s.usage = &provider.Usage{
				PromptTokens:     resp.UsageMetadata.PromptTokenCount,
				CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      resp.UsageMetadata.TotalTokenCount,
			}
		}

		if len(resp.Candidates) == 0 {
			continue
		}

		candidate := resp.Candidates[0]
		var content string
		var toolCalls []provider.ToolCall

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				content += part.Text
			}
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args) //nolint:errcheck // known-good struct
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

		finishReason := ""
		if candidate.FinishReason != "" {
			finishReason = mapFinishReason(candidate.FinishReason)
		}

		return &provider.StreamChunk{
			ID:       fmt.Sprintf("vertex-%d", time.Now().UnixNano()),
			Provider: "vertex",
			Model:    s.model,
			Delta: provider.Delta{
				Content:   content,
				ToolCalls: toolCalls,
			},
			FinishReason: finishReason,
		}, nil
	}

	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("vertex: stream read error: %w", err)
	}

	s.done = true
	return nil, io.EOF
}

func (s *vertexStream) Close() error {
	s.done = true
	return s.body.Close()
}

func (s *vertexStream) Usage() *provider.Usage {
	return s.usage
}

// Compile-time check.
var _ provider.Stream = (*vertexStream)(nil)

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/nexus/provider"
)

// officialBaseURL is OpenAI's own API endpoint, the provider's default.
const officialBaseURL = "https://api.openai.com/v1"

type client struct {
	apiKey  string
	baseURL string
	orgID   string
	http    *http.Client

	// maxCompletionTokens sends max_completion_tokens on any base URL
	// (WithMaxCompletionTokens). OpenAI's own endpoint always gets it.
	maxCompletionTokens bool
}

func newClient(apiKey, baseURL, orgID string, timeout time.Duration) *client {
	return &client{
		apiKey:  apiKey,
		baseURL: baseURL,
		orgID:   orgID,
		http:    newHTTPClient(timeout),
	}
}

// newHTTPClient bounds only the wait for response headers. A whole-request
// timeout would also cut a long stream after its headers arrived, so the
// client sets none and leaves the rest to the caller's context.
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: newTransport(http.DefaultTransport, timeout)}
}

// newTransport is a copy of def, or the stock settings when def is not an
// *http.Transport, with the response-header timeout set.
func newTransport(def http.RoundTripper, timeout time.Duration) *http.Transport {
	var transport *http.Transport
	if base, ok := def.(*http.Transport); ok {
		transport = base.Clone()
	} else {
		// Someone replaced http.DefaultTransport: start from the stock
		// settings, so dial, TLS and idle timeouts and HTTP/2 still apply.
		transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
	transport.ResponseHeaderTimeout = timeout
	return transport
}

// effective returns the key, base URL and organization one request is sent
// with. A context credential (provider.WithCredential) with a base URL
// brings its own key, empty or not: the configured key never goes to a host
// the credential chose. One with only a key keeps the configured base URL.
// The configured organization goes only with the configured key; a
// tenant's key is not in the operator's organization.
func (c *client) effective(ctx context.Context) (key, base, org string) {
	cred, ok := provider.CredentialFromContext(ctx)
	switch {
	case !ok:
		return c.apiKey, c.baseURL, c.orgID
	case cred.BaseURL != "":
		return cred.APIKey, strings.TrimRight(cred.BaseURL, "/"), ""
	default:
		return cred.APIKey, c.baseURL, ""
	}
}

// isOfficial reports whether base is OpenAI's own endpoint.
func isOfficial(base string) bool {
	return strings.TrimRight(base, "/") == officialBaseURL
}

// sendsMaxCompletionTokens reports whether a request sent to base caps its
// output with max_completion_tokens. OpenAI's own endpoint wants it (its
// reasoning models reject max_tokens); many compatible servers (Ollama,
// vLLM, LM Studio) still expect max_tokens, so elsewhere it is opt-in.
func (c *client) sendsMaxCompletionTokens(base string) bool {
	return c.maxCompletionTokens || isOfficial(base)
}

// newRequest builds a request for path on the effective base URL, carrying
// the effective key, and returns that key so an error can leave it out. It
// fails before any I/O when there is no base URL.
func (c *client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, string, error) {
	key, base, org := c.effective(ctx)
	if base == "" {
		return nil, "", fmt.Errorf("openai: %w", provider.ErrMissingBaseURL)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, "", fmt.Errorf("openai: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if org != "" {
		req.Header.Set("OpenAI-Organization", org)
	}
	return req, key, nil
}

// apiError reports a non-200 answer. The body is kept for the cause, with
// the key the request was sent with cut out of it: some servers echo the
// key they rejected.
// minRedactedKey is the shortest key cut out of an error body. A shorter
// one (a placeholder like "k" for a server that takes no key) would mangle
// the body without hiding a real secret.
const minRedactedKey = 8

func apiError(status int, body []byte, key string) error {
	text := string(body)
	if len(key) >= minRedactedKey {
		text = strings.ReplaceAll(text, key, "[redacted]")
	}
	return fmt.Errorf("openai: API error (status %d): %s", status, text)
}

// openAIRequest is the OpenAI chat completion request format.
type openAIRequest struct {
	Model               string                   `json:"model"`
	Messages            []openAIMessage          `json:"messages"`
	MaxTokens           int                      `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                      `json:"max_completion_tokens,omitempty"`
	Temperature         *float64                 `json:"temperature,omitempty"`
	TopP                *float64                 `json:"top_p,omitempty"`
	Stop                []string                 `json:"stop,omitempty"`
	Stream              bool                     `json:"stream,omitempty"`
	StreamOptions       *openAIStreamOptions     `json:"stream_options,omitempty"`
	Tools               []provider.Tool          `json:"tools,omitempty"`
	ToolChoice          any                      `json:"tool_choice,omitempty"`
	ResponseFormat      *provider.ResponseFormat `json:"response_format,omitempty"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type openAIMessage struct {
	Role             string              `json:"role"`
	Content          any                 `json:"content"`
	Name             string              `json:"name,omitempty"`
	ToolCalls        []provider.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string              `json:"tool_call_id,omitempty"`
	Reasoning        string              `json:"reasoning,omitempty"`
	ReasoningContent string              `json:"reasoning_content,omitempty"`
	Refusal          string              `json:"refusal,omitempty"`
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
	Usage openAIUsage `json:"usage"`
}

// openAIUsage is a chat completion's token usage. prompt_tokens counts the
// cached and cache-written tokens too.
type openAIUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens     int `json:"cached_tokens"`
		CacheWriteTokens int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// toUsage maps OpenAI's usage, cache reads and writes included, onto
// provider.Usage. The cache counts are parts of PromptTokens there too.
func (u openAIUsage) toUsage() provider.Usage {
	out := provider.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
	if d := u.PromptTokensDetails; d != nil {
		out.CacheReadTokens = d.CachedTokens
		out.CacheWriteTokens = d.CacheWriteTokens
	}
	return out
}

func (c *client) complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	_, base, _ := c.effective(ctx)
	oaiReq, err := c.toOpenAIRequest(req, c.sendsMaxCompletionTokens(base))
	if err != nil {
		return nil, err
	}
	oaiReq.Stream = false

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, key, err := c.newRequest(ctx, http.MethodPost, "/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	start := time.Now()
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	elapsed := time.Since(start)

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body) //nolint:errcheck // best-effort read for error message
		return nil, apiError(httpResp.StatusCode, respBody, key)
	}

	var oaiResp openAIResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("openai: decode response: %w", err)
	}

	return c.fromOpenAIResponse(&oaiResp, elapsed), nil
}

func (c *client) completeStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	_, base, _ := c.effective(ctx)
	oaiReq, err := c.toOpenAIRequest(req, c.sendsMaxCompletionTokens(base))
	if err != nil {
		return nil, err
	}
	oaiReq.Stream = true
	// Ask OpenAI to emit a final chunk carrying token usage. Without this,
	// streaming requests silently bypass billing.
	oaiReq.StreamOptions = &openAIStreamOptions{IncludeUsage: true}

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, key, err := c.newRequest(ctx, http.MethodPost, "/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer func() { _ = httpResp.Body.Close() }()
		respBody, _ := io.ReadAll(httpResp.Body) //nolint:errcheck // best-effort read for error message
		return nil, apiError(httpResp.StatusCode, respBody, key)
	}

	return newOpenAIStream(ctx, httpResp.Body, req.Model), nil
}

func (c *client) embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	payload := map[string]any{
		"model": req.Model,
		"input": req.Input,
	}
	if req.Dimensions > 0 {
		payload["dimensions"] = req.Dimensions
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, key, err := c.newRequest(ctx, http.MethodPost, "/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body) //nolint:errcheck // best-effort read for error message
		return nil, apiError(httpResp.StatusCode, respBody, key)
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
	httpReq, _, err := c.newRequest(ctx, http.MethodGet, "/models", http.NoBody)
	if err != nil {
		return err
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	_, _ = io.ReadAll(httpResp.Body) //nolint:errcheck // drain body before close

	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("openai: health check failed with status %d", httpResp.StatusCode)
	}
	return nil
}

// toOpenAIRequest builds the chat request. completionTokens sends the
// output cap as max_completion_tokens instead of max_tokens. A model that
// rejects sampling parameters (traits) is sent neither temperature nor
// top_p. The tool choice is sent in OpenAI's shape whichever shape the
// caller used; one that cannot be read fails before any request is sent.
func (c *client) toOpenAIRequest(req *provider.CompletionRequest, completionTokens bool) (*openAIRequest, error) {
	toolChoice, err := openAIToolChoice(req.ToolChoice)
	if err != nil {
		return nil, err
	}
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

	out := &openAIRequest{
		Model:          req.Model,
		Messages:       messages,
		Temperature:    req.Temperature,
		TopP:           req.TopP,
		Stop:           req.Stop,
		Tools:          req.Tools,
		ToolChoice:     toolChoice,
		ResponseFormat: req.ResponseFormat,
	}
	if completionTokens {
		out.MaxCompletionTokens = req.MaxTokens
	} else {
		out.MaxTokens = req.MaxTokens
	}
	if t, _ := modelTraitsFor(req.Model); t.reasoning {
		out.Temperature, out.TopP = nil, nil
	}
	return out, nil
}

// openAIToolChoice returns a CompletionRequest.ToolChoice in OpenAI's
// shape. Objects OpenAI defines that nexus does not model pass through
// unchanged; see provider.OpenAIToolChoice.
func openAIToolChoice(v any) (any, error) {
	tc, err := provider.OpenAIToolChoice(v)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	return tc, nil
}

func (c *client) fromOpenAIResponse(resp *openAIResponse, elapsed time.Duration) *provider.CompletionResponse {
	choices := make([]provider.Choice, len(resp.Choices))
	for i, ch := range resp.Choices {
		// Some reasoning-style models (e.g. nvidia/nemotron-3-nano-omni,
		// magistral) emit their visible output in `reasoning_content`
		// instead of `content` — often the case for json_schema strict
		// mode where the model's only output is the schema-conformant
		// JSON wrapped in its thinking trace. Fall back to those fields
		// so CompleteJSON (and any other downstream parser that reads
		// Choice.Message.Content) sees the actual model output instead
		// of an empty string while completion_tokens > 0.
		content := ch.Message.Content
		if isEmptyContent(content) {
			switch {
			case ch.Message.ReasoningContent != "":
				content = ch.Message.ReasoningContent
			case ch.Message.Reasoning != "":
				content = ch.Message.Reasoning
			case ch.Message.Refusal != "":
				content = ch.Message.Refusal
			}
		}
		choices[i] = provider.Choice{
			Index: ch.Index,
			Message: provider.Message{
				Role:      ch.Message.Role,
				Content:   content,
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
		Usage:    resp.Usage.toUsage(),
		Latency:  elapsed,
	}
}

// isEmptyContent reports whether the parsed content value is effectively
// empty — either nil, the empty string, or an empty multimodal parts
// slice. Used by fromOpenAIResponse to decide whether to substitute a
// reasoning-style field as a fallback.
func isEmptyContent(v any) bool {
	switch c := v.(type) {
	case nil:
		return true
	case string:
		return c == ""
	case []any:
		return len(c) == 0
	}
	return false
}

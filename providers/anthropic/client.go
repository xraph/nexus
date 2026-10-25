package anthropic

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

type client struct {
	apiKey     string
	baseURL    string
	apiVersion string
	http       *http.Client
	// blockBinding is the thinking block_binding mode, or "" for none.
	blockBinding string
	// cache is the prompt caching setup, or nil for none.
	cache *CacheConfig
}

func newClient(apiKey, baseURL, apiVersion string, timeout time.Duration) *client {
	return &client{
		apiKey:     apiKey,
		baseURL:    baseURL,
		apiVersion: apiVersion,
		http:       newHTTPClient(timeout),
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

// Anthropic messages API request format.
type anthropicRequest struct {
	Model         string               `json:"model"`
	Messages      []anthropicMessage   `json:"messages"`
	System        any                  `json:"system,omitempty"` // string, or []anthropicSystemBlock with caching on
	MaxTokens     int                  `json:"max_tokens"`
	Stream        bool                 `json:"stream,omitempty"`
	Temperature   *float64             `json:"temperature,omitempty"`
	TopP          *float64             `json:"top_p,omitempty"`
	StopSequences []string             `json:"stop_sequences,omitempty"`
	Tools         []anthropicTool      `json:"tools,omitempty"`
	Thinking      *anthropicThinking   `json:"thinking,omitempty"`
	ToolChoice    *anthropicToolChoice `json:"tool_choice,omitempty"`

	// CacheControl is the automatic breakpoint on the conversation tail.
	CacheControl *cacheControl `json:"cache_control,omitempty"`

	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
}

// anthropicToolChoice is the tool_choice request field: type is auto, none,
// any or tool, and name is set only for tool.
type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// anthropicRequestBlock is a content block in an outbound message. Anthropic
// has no "tool" role: a tool call is a tool_use block on an assistant message,
// and a tool result is a tool_result block on a user message.
type anthropicRequestBlock struct {
	Type string `json:"type"` // text, tool_use, tool_result

	// text
	Text string `json:"text,omitempty"`

	// tool_use
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`

	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema,omitempty"`

	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type anthropicResponse struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Role  string `json:"role"`
	Model string `json:"model"`
	// Content is kept as the API returned it, for Message.Native; the
	// unified fields are read from its decoded blocks.
	Content    json.RawMessage `json:"content"`
	StopReason string          `json:"stop_reason"`
	Usage      anthropicUsage  `json:"usage"`
}

// anthropicUsage is the Messages API usage object. input_tokens counts only
// the uncached part of the prompt; cache reads and writes are reported
// beside it.
type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheCreation            struct {
		Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
		Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

// toProvider maps the usage onto provider.Usage, whose PromptTokens includes
// cache reads and writes and whose cache counts are subsets of it.
func (u *anthropicUsage) toProvider() provider.Usage {
	writes := max(u.CacheCreationInputTokens, u.CacheCreation.Ephemeral5mInputTokens+u.CacheCreation.Ephemeral1hInputTokens)
	prompt := u.InputTokens + u.CacheReadInputTokens + writes
	return provider.Usage{
		PromptTokens:         prompt,
		CompletionTokens:     u.OutputTokens,
		CacheReadTokens:      u.CacheReadInputTokens,
		CacheWriteTokens:     writes,
		CacheWriteLongTokens: u.CacheCreation.Ephemeral1hInputTokens,
		TotalTokens:          prompt + u.OutputTokens,
	}
}

type anthropicContentBlock struct {
	Type     string `json:"type"` // text, tool_use, thinking
	Text     string `json:"text,omitempty"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Input    any    `json:"input,omitempty"`
	Thinking string `json:"thinking,omitempty"`
}

func (c *client) complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	key, base := c.effective(ctx)
	if err := requireEndpoint(key, base); err != nil {
		return nil, err
	}

	antReq, err := c.toAnthropicRequest(req, false)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(antReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	c.setHeaders(httpReq, key)
	setBetaHeaders(httpReq, antReq)

	start := time.Now()
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	elapsed := time.Since(start)

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(httpResp.Body) //nolint:errcheck // best-effort read for error message
		return nil, apiError(httpResp.StatusCode, respBody, key)
	}

	var antResp anthropicResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&antResp); err != nil {
		return nil, fmt.Errorf("anthropic: decode response: %w", err)
	}
	var blocks []anthropicContentBlock
	if len(antResp.Content) > 0 {
		if err := json.Unmarshal(antResp.Content, &blocks); err != nil {
			return nil, fmt.Errorf("anthropic: decode response content: %w", err)
		}
	}

	return c.fromAnthropicResponse(&antResp, blocks, elapsed), nil
}

func (c *client) completeStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	key, base := c.effective(ctx)
	if err := requireEndpoint(key, base); err != nil {
		return nil, err
	}

	antReq, err := c.toAnthropicRequest(req, true)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(antReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	c.setHeaders(httpReq, key)
	setBetaHeaders(httpReq, antReq)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer func() { _ = httpResp.Body.Close() }()
		respBody, _ := io.ReadAll(httpResp.Body) //nolint:errcheck // best-effort read for error message
		return nil, apiError(httpResp.StatusCode, respBody, key)
	}

	return newAnthropicStream(ctx, httpResp.Body, req.Model), nil
}

func (c *client) ping(ctx context.Context) error {
	// Anthropic doesn't have a dedicated health endpoint.
	// We send a minimal request to check connectivity.
	key, base := c.effective(ctx)
	if base == "" {
		return fmt.Errorf("anthropic: %w", provider.ErrMissingBaseURL)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/messages", http.NoBody)
	if err != nil {
		return err
	}
	c.setHeaders(httpReq, key)

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = httpResp.Body.Close() }()
	_, _ = io.ReadAll(httpResp.Body) //nolint:errcheck // drain body before close

	// 405 Method Not Allowed is fine — endpoint exists
	if httpResp.StatusCode == http.StatusMethodNotAllowed || httpResp.StatusCode == http.StatusOK {
		return nil
	}
	// 401 means the key is invalid but the server is reachable
	if httpResp.StatusCode == http.StatusUnauthorized {
		return nil
	}
	return fmt.Errorf("anthropic: health check failed with status %d", httpResp.StatusCode)
}

// effective returns the key and base URL one request is sent with. A
// context credential (provider.WithCredential) with a base URL brings its
// own key, empty or not: the configured key never goes to a host the
// credential chose. One with only a key keeps the configured base URL.
func (c *client) effective(ctx context.Context) (key, base string) {
	cred, ok := provider.CredentialFromContext(ctx)
	switch {
	case !ok:
		return c.apiKey, c.baseURL
	case cred.BaseURL != "":
		return cred.APIKey, strings.TrimRight(cred.BaseURL, "/")
	default:
		return cred.APIKey, c.baseURL
	}
}

// requireEndpoint fails fast when the request has no key or no base URL.
// Anthropic authenticates with the x-api-key header; sending it empty yields
// a confusing upstream 401 ("x-api-key header is required"), so we surface a
// clear local error before any network round-trip instead.
func requireEndpoint(key, base string) error {
	if key == "" {
		return fmt.Errorf("anthropic: %w", provider.ErrMissingAPIKey)
	}
	if base == "" {
		return fmt.Errorf("anthropic: %w", provider.ErrMissingBaseURL)
	}
	return nil
}

// apiError reports a non-200 answer. The body is kept for the cause, with
// the key the request was sent with cut out of it, in case it is echoed.
// minRedactedKey is the shortest key cut out of an error body. A shorter
// one (a placeholder like "k" for a server that takes no key) would mangle
// the body without hiding a real secret.
const minRedactedKey = 8

func apiError(status int, body []byte, key string) error {
	text := string(body)
	if len(key) >= minRedactedKey {
		text = strings.ReplaceAll(text, key, "[redacted]")
	}
	return fmt.Errorf("anthropic: API error (status %d): %s", status, text)
}

func (c *client) setHeaders(req *http.Request, key string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", c.apiVersion)
}

// setBetaHeaders adds the anthropic-beta headers the request body needs.
func setBetaHeaders(req *http.Request, antReq *anthropicRequest) {
	if antReq.Thinking != nil && antReq.Thinking.BlockBinding != nil {
		req.Header.Add("anthropic-beta", blockBindingBeta)
	}
}

// toAnthropicRequest shapes the request for the model: thinking, effort,
// sampling and max_tokens follow the model's traits. It fails, before any
// call, when the model does not accept the requested effort.
func (c *client) toAnthropicRequest(req *provider.CompletionRequest, stream bool) (*anthropicRequest, error) {
	tr, listed := traitsFor(req.Model)
	effort, err := effortFor(req.Model, tr, listed, req.Thinking)
	if err != nil {
		return nil, err
	}

	messages := make([]anthropicMessage, 0, len(req.Messages))

	// Extract system message, and translate OpenAI-style roles to the
	// Anthropic shape. Anthropic only accepts "user" and "assistant": a
	// "tool" result message becomes a user message with a tool_result block,
	// and an assistant tool call becomes a tool_use block.
	system := req.System
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			if s, ok := m.Content.(string); ok {
				if system == "" {
					system = s
				} else {
					system += "\n\n" + s
				}
			}

		case "tool":
			// OpenAI sends tool output as its own message keyed by
			// tool_call_id; Anthropic carries it as a tool_result block on a
			// user turn referencing the originating tool_use.
			messages = append(messages, anthropicMessage{
				Role: "user",
				Content: []anthropicRequestBlock{{
					Type:      "tool_result",
					ToolUseID: m.ToolCallID,
					Content:   messageText(m.Content),
				}},
			})

		case "assistant":
			// An untouched reply goes back exactly as the API returned it,
			// so its signed thinking blocks stay valid. An edited one is
			// rebuilt below, without thinking.
			if native := nativeReplay(&m); native != nil {
				messages = append(messages, anthropicMessage{Role: "assistant", Content: native})
				break
			}
			if len(m.ToolCalls) == 0 {
				messages = append(messages, anthropicMessage{Role: "assistant", Content: m.Content})
				break
			}
			// An assistant turn that calls tools must serialize each call as a
			// tool_use block (with the arguments as a JSON object, not a
			// string), preceded by any accompanying text.
			blocks := make([]anthropicRequestBlock, 0, len(m.ToolCalls)+1)
			if text := messageText(m.Content); text != "" {
				blocks = append(blocks, anthropicRequestBlock{Type: "text", Text: text})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropicRequestBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: toolInput(tc.Function.Arguments),
				})
			}
			messages = append(messages, anthropicMessage{Role: "assistant", Content: blocks})

		default: // "user" and anything else
			messages = append(messages, anthropicMessage{Role: m.Role, Content: m.Content})
		}
	}

	thinking := thinkingFor(tr, listed, req.Thinking)
	// The API accepts block_binding only on adaptive thinking.
	if c.blockBinding != "" && thinking != nil && thinking.Type == "adaptive" {
		thinking.BlockBinding = &anthropicBlockBinding{PrefixMismatchBehavior: c.blockBinding}
	}
	antReq := &anthropicRequest{
		Model:         req.Model,
		Messages:      messages,
		MaxTokens:     maxTokensFor(req, stream, thinking),
		Stream:        stream,
		StopSequences: req.Stop,
		Thinking:      thinking,
	}
	if effort != "" {
		antReq.OutputConfig = &anthropicOutputConfig{Effort: effort}
	}
	// Models with fixed sampling, and every model on 2026-10-21, reject
	// temperature and top_p even at their defaults.
	if !tr.fixedSampling && c.apiVersion != APIVersion20261021 {
		antReq.Temperature = req.Temperature
		antReq.TopP = req.TopP
	}

	// Convert tools
	for _, t := range req.Tools {
		antReq.Tools = append(antReq.Tools, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}

	c.cache.apply(antReq, system)

	toolChoice, err := toolChoiceFor(req, tr)
	if err != nil {
		return nil, err
	}
	antReq.ToolChoice = toolChoice

	return antReq, nil
}

// toolChoiceFor maps the request's OpenAI-shaped ToolChoice to Anthropic's.
// It returns nil when there is no choice or no tools to choose from, since
// the API rejects tool_choice without tools. A forced choice on a model that
// rejects it fails here, before any request is sent.
func toolChoiceFor(req *provider.CompletionRequest, tr traits) (*anthropicToolChoice, error) {
	tc, ok, err := provider.ParseToolChoice(req.ToolChoice)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	if !ok || len(req.Tools) == 0 {
		return nil, nil
	}
	switch tc.Mode {
	case provider.ToolChoiceNone:
		return &anthropicToolChoice{Type: "none"}, nil
	case provider.ToolChoiceRequired, provider.ToolChoiceFunction:
		if !tr.forcedToolChoice {
			return nil, fmt.Errorf("%w (model %s)", ErrForcedToolChoice, req.Model)
		}
		if tc.Mode == provider.ToolChoiceFunction {
			return &anthropicToolChoice{Type: "tool", Name: tc.Function}, nil
		}
		return &anthropicToolChoice{Type: "any"}, nil
	default:
		return &anthropicToolChoice{Type: "auto"}, nil
	}
}

// messageText flattens a message's Content to a plain string for use inside a
// tool_result block or as text content. Strings pass through; multimodal parts
// contribute their text; anything else falls back to its JSON encoding.
func messageText(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []provider.ContentPart:
		var b strings.Builder
		for _, p := range v {
			b.WriteString(p.Text)
		}
		return b.String()
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(raw)
	}
}

// toolInput converts an OpenAI tool-call arguments string (always JSON) into
// the JSON object Anthropic expects for a tool_use block's `input`. Empty
// arguments become an empty object; malformed JSON is passed through as a
// string so the request still marshals (Anthropic then reports the schema
// error) rather than failing to build the body at all.
func toolInput(arguments string) any {
	s := strings.TrimSpace(arguments)
	if s == "" {
		return json.RawMessage("{}")
	}
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	return s
}

func (c *client) fromAnthropicResponse(resp *anthropicResponse, blocks []anthropicContentBlock, elapsed time.Duration) *provider.CompletionResponse {
	var content string
	var thinkingContent string
	var toolCalls []provider.ToolCall

	for _, block := range blocks {
		switch block.Type {
		case "text":
			content += block.Text
		case "thinking":
			thinkingContent += block.Thinking
		case "tool_use":
			inputJSON, _ := json.Marshal(block.Input) //nolint:errcheck // known-good struct
			toolCalls = append(toolCalls, provider.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: provider.ToolCallFunc{
					Name:      block.Name,
					Arguments: string(inputJSON),
				},
			})
		}
	}

	var native *provider.NativeContent
	if len(blocks) > 0 {
		native = newNative(resp.Model, resp.Content)
	}

	return &provider.CompletionResponse{
		ID:       resp.ID,
		Provider: "anthropic",
		Model:    resp.Model,
		Created:  time.Now(),
		Choices: []provider.Choice{{
			Index: 0,
			Message: provider.Message{
				Role:      "assistant",
				Content:   content,
				ToolCalls: toolCalls,
				Native:    native,
			},
			FinishReason: mapStopReason(resp.StopReason),
		}},
		Usage:           resp.Usage.toProvider(),
		Latency:         elapsed,
		ThinkingContent: thinkingContent,
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
	case "refusal":
		return "content_filter"
	case "pause_turn":
		return "pause_turn"
	default:
		return reason
	}
}

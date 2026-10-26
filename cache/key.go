package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"sort"

	"github.com/xraph/nexus/provider"
)

// Key generates a deterministic cache key from a request.
// The key is a SHA-256 hash of every request field that can change the
// answer: the tenant, the named provider, the model, the system prompt,
// each message (role, content, name, tool calls and tool call id), the
// sampling parameters, the tools, the tool choice, the thinking settings
// and the response format. Tenants never share entries, and neither do
// requests routed to different providers.
//
// Each field is written as JSON, whose encoder sorts map keys, so the same
// request always gives the same key. A tool choice is keyed by what it
// means, so "required" and Anthropic's {"type":"any"} share an entry.
//
// Key returns "" when a field cannot be encoded. Such a request must not be
// cached.
func Key(req *provider.CompletionRequest) string {
	h := sha256.New()

	// Tenant: one tenant must never be served another's cached completion.
	_, _ = fmt.Fprintf(h, "tenant:%s\n", req.TenantID)

	// Provider: a request that names a provider routes there, so it must
	// never be served another provider's answer, nor one routed by model.
	_, _ = fmt.Fprintf(h, "provider:%s\n", req.Provider)

	// Model
	_, _ = fmt.Fprintf(h, "model:%s\n", req.Model)

	// System prompt
	if req.System != "" && !writeJSON(h, "system", req.System) {
		return ""
	}

	// Messages. Two agent turns that differ only in a tool call's id or
	// arguments, or in which call a tool result answers, are different
	// conversations.
	for _, msg := range req.Messages {
		if !writeJSON(h, "msg", keyMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
		}) {
			return ""
		}
	}

	// Temperature
	if req.Temperature != nil {
		_, _ = fmt.Fprintf(h, "temp:%f\n", *req.Temperature)
	}

	// MaxTokens
	if req.MaxTokens > 0 {
		_, _ = fmt.Fprintf(h, "max_tokens:%d\n", req.MaxTokens)
	}

	// TopP
	if req.TopP != nil {
		_, _ = fmt.Fprintf(h, "top_p:%f\n", *req.TopP)
	}

	// Stop sequences
	if len(req.Stop) > 0 {
		sorted := make([]string, len(req.Stop))
		copy(sorted, req.Stop)
		sort.Strings(sorted)
		if !writeJSON(h, "stop", sorted) {
			return ""
		}
	}

	// Tools (if present, changes the response)
	if len(req.Tools) > 0 && !writeJSON(h, "tools", req.Tools) {
		return ""
	}

	// Tool choice
	if req.ToolChoice != nil && !writeJSON(h, "tool_choice", toolChoiceKey(req.ToolChoice)) {
		return ""
	}

	// Thinking, effort included
	if req.Thinking != nil && !writeJSON(h, "thinking", req.Thinking) {
		return ""
	}

	// Response format
	if req.ResponseFormat != nil && !writeJSON(h, "response_format", req.ResponseFormat) {
		return ""
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// keyMessage is the part of a message that changes the answer. Native
// content is left out: it repeats the text and tool calls in the
// provider's own format.
type keyMessage struct {
	Role       string              `json:"role"`
	Content    any                 `json:"content"`
	Name       string              `json:"name,omitempty"`
	ToolCalls  []provider.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
}

// toolChoiceKey is the tool choice in one shape whatever shape the caller
// used. A value ParseToolChoice cannot read is keyed as it is; the provider
// refuses it anyway.
func toolChoiceKey(v any) any {
	tc, ok, err := provider.ParseToolChoice(v)
	if err != nil {
		return v
	}
	if !ok {
		return nil
	}
	return tc
}

// writeJSON writes "label:<json>\n" to h. JSON quotes strings, so no field
// can run into the next. It reports false when v cannot be encoded.
func writeJSON(h hash.Hash, label string, v any) bool {
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	_, _ = fmt.Fprintf(h, "%s:%s\n", label, data)
	return true
}

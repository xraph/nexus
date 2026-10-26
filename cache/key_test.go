package cache_test

import (
	"testing"

	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/provider"
)

func TestTenantsDoNotShareCacheKeys(t *testing.T) {
	a := &provider.CompletionRequest{Model: "gpt-4o", TenantID: "tenant_a", Messages: []provider.Message{{Role: "user", Content: "hi"}}}
	b := *a
	b.TenantID = "tenant_b"
	if cache.Key(a) == cache.Key(&b) {
		t.Fatalf("two tenants got the same cache key")
	}
	if cache.StreamKey(a) == cache.StreamKey(&b) {
		t.Fatalf("two tenants got the same stream cache key")
	}
}

func TestProvidersDoNotShareCacheKeys(t *testing.T) {
	base := provider.CompletionRequest{Model: "gpt-4o", TenantID: "tenant_a", Messages: []provider.Message{{Role: "user", Content: "hi"}}}
	keys := map[string]string{}
	streamKeys := map[string]string{}
	for _, name := range []string{"", "openai", "openrouter"} {
		req := base
		req.Provider = name
		if other, ok := keys[cache.Key(&req)]; ok {
			t.Fatalf("provider %q got the same cache key as provider %q", name, other)
		}
		if other, ok := streamKeys[cache.StreamKey(&req)]; ok {
			t.Fatalf("provider %q got the same stream cache key as provider %q", name, other)
		}
		keys[cache.Key(&req)] = name
		streamKeys[cache.StreamKey(&req)] = name

		same := req
		if cache.Key(&req) != cache.Key(&same) || cache.StreamKey(&req) != cache.StreamKey(&same) {
			t.Fatalf("identical requests naming provider %q got different keys", name)
		}
	}
}

// agentTurn is a tool-use conversation: the assistant called a tool, with
// no text, and the tool answered.
func agentTurn(callID, args string) *provider.CompletionRequest {
	return &provider.CompletionRequest{
		Model:    "claude-opus-5-5",
		TenantID: "tenant_a",
		Messages: []provider.Message{
			{Role: "user", Content: "what is the weather in Paris?"},
			{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{ID: callID, Type: "function",
				Function: provider.ToolCallFunc{Name: "get_weather", Arguments: args}}}},
			{Role: "tool", ToolCallID: callID, Content: "sunny"},
		},
	}
}

func TestToolCallsChangeTheCacheKey(t *testing.T) {
	base := agentTurn("call_1", `{"city":"Paris"}`)
	if cache.Key(base) != cache.Key(agentTurn("call_1", `{"city":"Paris"}`)) {
		t.Fatal("identical agent turns got different keys")
	}
	for name, other := range map[string]*provider.CompletionRequest{
		"tool call id":        agentTurn("call_2", `{"city":"Paris"}`),
		"tool call arguments": agentTurn("call_1", `{"city":"Lyon"}`),
	} {
		if cache.Key(base) == cache.Key(other) {
			t.Errorf("%s: two requests that differ only in it got the same key", name)
		}
		if cache.StreamKey(base) == cache.StreamKey(other) {
			t.Errorf("%s: two requests that differ only in it got the same stream key", name)
		}
	}
}

func TestEveryAnswerChangingFieldIsInTheCacheKey(t *testing.T) {
	change := map[string]func(r *provider.CompletionRequest){
		"system":       func(r *provider.CompletionRequest) { r.System = "answer in French" },
		"tool choice":  func(r *provider.CompletionRequest) { r.ToolChoice = "required" },
		"thinking":     func(r *provider.CompletionRequest) { r.Thinking = &provider.ThinkingConfig{Enabled: true} },
		"effort":       func(r *provider.CompletionRequest) { r.Thinking = &provider.ThinkingConfig{Effort: "high"} },
		"format":       func(r *provider.CompletionRequest) { r.ResponseFormat = &provider.ResponseFormat{Type: "json_object"} },
		"message name": func(r *provider.CompletionRequest) { r.Messages[0].Name = "alice" },
		"tool call id": func(r *provider.CompletionRequest) { r.Messages[2].ToolCallID = "call_9" },
		"tool calls": func(r *provider.CompletionRequest) {
			r.Messages[1].ToolCalls = append(r.Messages[1].ToolCalls, provider.ToolCall{ID: "call_3", Type: "function"})
		},
	}
	seen := map[string]string{cache.Key(agentTurn("call_1", "{}")): "base"}
	for name, fn := range change {
		r := agentTurn("call_1", "{}")
		fn(r)
		k := cache.Key(r)
		if other, dup := seen[k]; dup {
			t.Errorf("%s: same key as %s", name, other)
		}
		seen[k] = name
	}
	effort := agentTurn("call_1", "{}")
	effort.Thinking = &provider.ThinkingConfig{Enabled: true, Effort: "low"}
	effortHigh := agentTurn("call_1", "{}")
	effortHigh.Thinking = &provider.ThinkingConfig{Enabled: true, Effort: "high"}
	if cache.Key(effort) == cache.Key(effortHigh) {
		t.Error("two efforts got the same key")
	}
}

func TestTheCacheKeyIsDeterministic(t *testing.T) {
	build := func(order []string) *provider.CompletionRequest {
		schema := map[string]any{}
		for _, k := range order {
			schema[k] = map[string]any{"type": "string"}
		}
		return &provider.CompletionRequest{
			Model:    "gpt-4o",
			Messages: []provider.Message{{Role: "user", Content: []provider.ContentPart{{Type: "text", Text: "hi"}}}},
			Tools: []provider.Tool{{Type: "function", Function: provider.ToolFunction{Name: "f",
				Parameters: map[string]any{"type": "object", "properties": schema}}}},
			ToolChoice:     map[string]any{"type": "function", "function": map[string]any{"name": "f"}},
			ResponseFormat: &provider.ResponseFormat{Type: "json_schema", Schema: schema},
		}
	}
	want := cache.Key(build([]string{"a", "b", "c", "d", "e"}))
	for range 20 {
		if got := cache.Key(build([]string{"e", "d", "c", "b", "a"})); got != want {
			t.Fatal("the same request built in another map order got another key")
		}
	}
}

func TestToolChoiceIsKeyedByMeaning(t *testing.T) {
	a := agentTurn("call_1", "{}")
	a.ToolChoice = "required"
	b := agentTurn("call_1", "{}")
	b.ToolChoice = map[string]any{"type": "any"}
	c := agentTurn("call_1", "{}")
	c.ToolChoice = provider.ToolChoice{Mode: provider.ToolChoiceRequired}
	if cache.Key(a) != cache.Key(b) || cache.Key(a) != cache.Key(c) {
		t.Fatal("three spellings of the same tool choice got different keys")
	}
}

func TestARequestThatCannotBeEncodedHasNoKey(t *testing.T) {
	r := &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: make(chan int)}}}
	if k := cache.Key(r); k != "" {
		t.Fatalf("Key = %q, want \"\" for a request that cannot be encoded", k)
	}
	if k := cache.StreamKey(r); k != "" {
		t.Fatalf("StreamKey = %q, want \"\"", k)
	}
}

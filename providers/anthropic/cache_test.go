package anthropic_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/anthropic"
	"github.com/xraph/nexus/testutil"
)

func cachingProvider(t *testing.T, c anthropic.CacheConfig) (*anthropic.Provider, *testutil.MockServer) {
	t.Helper()
	mock := testutil.NewMockServer(t)
	p := anthropic.New("k", anthropic.WithBaseURL(mock.Server.URL), anthropic.WithPromptCaching(c))
	return p, mock
}

func threeTools() []provider.Tool {
	tools := make([]provider.Tool, 0, 3)
	for _, n := range []string{"a", "b", "c"} {
		tools = append(tools, provider.Tool{Type: "function", Function: provider.ToolFunction{
			Name: n, Parameters: map[string]any{"type": "object"},
		}})
	}
	return tools
}

func TestCachingOffSendsSystemAsAString(t *testing.T) {
	p, mock := newMockProvider(t)
	completeWith(t, mock, p, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5", System: "be brief", Tools: threeTools(),
	})
	if got := string(lastBody(t, mock)["system"]); got != `"be brief"` {
		t.Errorf("system = %s, want a string", got)
	}
	if raw := string(mock.Ctrl.GetLastBody()); strings.Contains(raw, "cache_control") {
		t.Errorf("cache_control present with caching off: %s", raw)
	}
}

func TestCachingMarksLastToolAndSystem(t *testing.T) {
	p, mock := cachingProvider(t, anthropic.CacheConfig{TTL: anthropic.CacheTTL5m})
	completeWith(t, mock, p, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5", System: "be brief", Tools: threeTools(),
	})
	body := lastBody(t, mock)
	if _, ok := body["cache_control"]; ok {
		t.Error("top-level cache_control set with Messages false")
	}
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(body["tools"], &tools); err != nil {
		t.Fatal(err)
	}
	for i, tool := range tools {
		cc, ok := tool["cache_control"]
		if i < 2 && ok {
			t.Errorf("tools[%d] has cache_control", i)
		}
		if i == 2 && string(cc) != `{"type":"ephemeral"}` {
			t.Errorf("tools[2] cache_control = %s", cc)
		}
	}
	want := `[{"type":"text","text":"be brief","cache_control":{"type":"ephemeral"}}]`
	if got := string(body["system"]); got != want {
		t.Errorf("system = %s, want %s", got, want)
	}
}

func TestCachingOneHourTTL(t *testing.T) {
	p, mock := cachingProvider(t, anthropic.CacheConfig{TTL: anthropic.CacheTTL1h, Messages: true})
	completeWith(t, mock, p, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5", System: "be brief", Tools: threeTools(),
	})
	body := lastBody(t, mock)
	const marker = `{"type":"ephemeral","ttl":"1h"}`
	if got := string(body["cache_control"]); got != marker {
		t.Errorf("top-level cache_control = %s", got)
	}
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(body["tools"], &tools); err != nil {
		t.Fatal(err)
	}
	if got := string(tools[2]["cache_control"]); got != marker {
		t.Errorf("tools[2] cache_control = %s", got)
	}
	var sys []map[string]json.RawMessage
	if err := json.Unmarshal(body["system"], &sys); err != nil {
		t.Fatal(err)
	}
	if got := string(sys[0]["cache_control"]); got != marker {
		t.Errorf("system cache_control = %s", got)
	}
}

func TestCachingWithoutSystemOrTools(t *testing.T) {
	p, mock := cachingProvider(t, anthropic.CacheConfig{TTL: anthropic.CacheTTL5m, Messages: true})
	completeWith(t, mock, p, &provider.CompletionRequest{Model: "claude-sonnet-4-5"})
	body := lastBody(t, mock)
	if _, ok := body["system"]; ok {
		t.Errorf("system present: %s", body["system"])
	}
	if _, ok := body["tools"]; ok {
		t.Errorf("tools present: %s", body["tools"])
	}
	if got := string(body["cache_control"]); got != `{"type":"ephemeral"}` {
		t.Errorf("top-level cache_control = %s", got)
	}
}

func TestCachingKeepsSystemMessagesMerged(t *testing.T) {
	p, mock := cachingProvider(t, anthropic.CacheConfig{TTL: anthropic.CacheTTL5m})
	completeWith(t, mock, p, &provider.CompletionRequest{
		Model:  "claude-sonnet-4-5",
		System: "one",
		Messages: []provider.Message{
			{Role: "system", Content: "two"},
			{Role: "system", Content: "three"},
			{Role: "user", Content: "hi"},
		},
	})
	var sys []map[string]any
	if err := json.Unmarshal(lastBody(t, mock)["system"], &sys); err != nil {
		t.Fatal(err)
	}
	if len(sys) != 1 || sys[0]["text"] != "one\n\ntwo\n\nthree" {
		t.Errorf("system = %v", sys)
	}
}

func TestCachingDoesNotTouchReplayedMessages(t *testing.T) {
	p, mock := cachingProvider(t, anthropic.CacheConfig{TTL: anthropic.CacheTTL5m, Messages: true})
	completeWith(t, mock, p, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5",
		Messages: []provider.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "yo"},
			{Role: "user", Content: "again"},
		},
	})
	var msgs []map[string]json.RawMessage
	if err := json.Unmarshal(lastBody(t, mock)["messages"], &msgs); err != nil {
		t.Fatal(err)
	}
	for i, m := range msgs {
		if strings.Contains(string(m["content"]), "cache_control") {
			t.Errorf("messages[%d] carries cache_control", i)
		}
	}
}

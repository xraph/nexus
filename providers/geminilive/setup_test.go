package geminilive

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xraph/nexus/provider"
)

func TestProvider_SetupForwardsRichConfig(t *testing.T) {
	t.Parallel()

	conn := &fakeConn{
		reads: [][]byte{
			mustJSON(map[string]any{"setupComplete": map[string]any{}}),
			mustJSON(map[string]any{"serverContent": map[string]any{"turnComplete": true}}),
		},
	}
	p := New("k",
		withDialer(func(_ context.Context, _, _ string) (wsConn, error) { return conn, nil }),
		WithSetup(SetupConfig{
			GenerationConfig: map[string]any{"temperature": 0.7},
			SystemInstruction: map[string]any{
				"parts": []map[string]any{{"text": "You are concise."}},
			},
			Tools: []any{map[string]any{"functionDeclarations": []any{}}},
		}),
	)

	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{Model: "models/gemini-2.0-flash-exp"})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.writes) < 1 {
		t.Fatal("no setup frame sent")
	}
	var first map[string]any
	if err := json.Unmarshal(conn.writes[0], &first); err != nil {
		t.Fatalf("decode setup: %v", err)
	}
	setup, ok := first["setup"].(map[string]any)
	if !ok {
		t.Fatalf("first write isn't a setup envelope: %v", first)
	}
	if setup["model"] != "models/gemini-2.0-flash-exp" {
		t.Fatalf("model lost: %v", setup["model"])
	}
	if _, ok := setup["generation_config"]; !ok {
		t.Fatalf("generation_config missing: %v", setup)
	}
	if _, ok := setup["system_instruction"]; !ok {
		t.Fatalf("system_instruction missing")
	}
	if _, ok := setup["tools"]; !ok {
		t.Fatalf("tools missing")
	}
}

func TestProvider_SetupOmitsZeroFields(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{
		reads: [][]byte{
			mustJSON(map[string]any{"setupComplete": map[string]any{}}),
			mustJSON(map[string]any{"serverContent": map[string]any{"turnComplete": true}}),
		},
	}
	p := New("k", withDialer(func(_ context.Context, _, _ string) (wsConn, error) { return conn, nil }))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	conn.mu.Lock()
	defer conn.mu.Unlock()
	var first map[string]any
	_ = json.Unmarshal(conn.writes[0], &first)
	setup := first["setup"].(map[string]any)
	for _, k := range []string{"generation_config", "system_instruction", "tools", "realtime_input_config"} {
		if _, ok := setup[k]; ok {
			t.Fatalf("%s should be omitted when not configured: %v", k, setup)
		}
	}
}

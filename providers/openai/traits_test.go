package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func TestEveryCatalogModelHasTraits(t *testing.T) {
	for _, m := range openAIModels() {
		if _, ok := modelTraitsFor(m.ID); !ok {
			t.Errorf("%s has no traits row", m.ID)
		}
	}
}

func TestOnlyEmbedding3TakesDimensions(t *testing.T) {
	for _, m := range openAIModels() {
		tr, _ := modelTraitsFor(m.ID)
		want := m.ID == "text-embedding-3-small" || m.ID == "text-embedding-3-large"
		if tr.embeddingDimensions != want {
			t.Errorf("%s: embeddingDimensions = %v, want %v", m.ID, tr.embeddingDimensions, want)
		}
	}
}

func TestReasoningModelsDropSampling(t *testing.T) {
	temp, topP := 0.2, 0.9
	cases := []struct {
		model    string
		dropping bool
	}{
		{"gpt-6-astra", true},
		{"gpt-5.5", true},
		{"gpt-5-mini-2025-08-07", true}, // a snapshot takes its model's traits
		{"o3", true},
		{"gpt-4o", false},
		{"gpt-5.4", false}, // reasoning effort defaults to none, which takes sampling
		{"some-local-model", false},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			srv := testutil.NewMockServer(t)
			p := New("k", WithBaseURL(srv.Server.URL))
			_, err := p.Complete(context.Background(), &provider.CompletionRequest{
				Model: tc.model, Temperature: &temp, TopP: &topP,
				Messages: []provider.Message{{Role: "user", Content: "hi"}},
			})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			var body map[string]any
			if err := json.Unmarshal(srv.Ctrl.GetLastBody(), &body); err != nil {
				t.Fatalf("body: %v", err)
			}
			_, hasTemp := body["temperature"]
			_, hasTopP := body["top_p"]
			if hasTemp == tc.dropping || hasTopP == tc.dropping {
				t.Fatalf("temperature sent %v, top_p sent %v; want both sent = %v", hasTemp, hasTopP, !tc.dropping)
			}
		})
	}
}

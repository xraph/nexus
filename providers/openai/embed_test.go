package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func TestOpenAISendsEmbeddingDimensions(t *testing.T) {
	for _, dims := range []int{512, 0} {
		srv := testutil.NewMockServer(t)
		srv.Ctrl.SetEmbedding(json.RawMessage(testutil.DefaultEmbeddingResponse()))
		p := New("k", WithBaseURL(srv.Server.URL))

		_, err := p.Embed(context.Background(), &provider.EmbeddingRequest{
			Model: "text-embedding-3-small", Input: []string{"hi"}, Dimensions: dims,
		})
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
		var body map[string]any
		if err := json.Unmarshal(srv.Ctrl.GetLastBody(), &body); err != nil {
			t.Fatalf("body: %v", err)
		}
		got, sent := body["dimensions"]
		switch {
		case dims > 0 && got != float64(dims):
			t.Fatalf("dimensions = %v, want %d", got, dims)
		case dims == 0 && sent:
			t.Fatalf("dimensions sent as %v with Dimensions 0; the model's default needs no key", got)
		}
	}
}

func TestOpenAISupportsDimensions(t *testing.T) {
	official := New("k")
	for model, want := range map[string]bool{
		"text-embedding-3-small": true,
		"text-embedding-3-large": true,
		"text-embedding-ada-002": false,
		"nomic-embed-text":       false,
	} {
		if got := official.SupportsDimensions(model); got != want {
			t.Errorf("api.openai.com: SupportsDimensions(%q) = %v, want %v", model, got, want)
		}
	}
	// A trailing slash is still OpenAI's own endpoint.
	if New("k", WithBaseURL("https://api.openai.com/v1/")).SupportsDimensions("text-embedding-ada-002") {
		t.Error("api.openai.com/v1/: ada-002 cannot shorten its vectors")
	}
	// Elsewhere the endpoint decides; the gateway checks the vector length.
	if !New("k", WithBaseURL("http://localhost:11434/v1")).SupportsDimensions("nomic-embed-text") {
		t.Error("a compatible base URL must pass dimensions through")
	}
	var _ provider.DimensionsAware = official
}

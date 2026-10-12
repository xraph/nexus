package middlewares_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
)

type failing struct{ listedFake }

func (failing) Complete(context.Context, *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, errors.New("upstream 502")
}

func TestProviderCallAttributesBeforeTheCall(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(failing{listedFake{name: "openai"}})
	mw := middlewares.NewProviderCall(nil, reg)
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{Model: "gpt-4o"}, State: map[string]any{}}
	if _, err := mw.Process(context.Background(), req, nil); err == nil {
		t.Fatalf("want the provider's error")
	}
	if req.State[pipeline.StateProviderName] != "openai" {
		t.Fatalf("a failed call must still say which provider was called, got %v", req.State[pipeline.StateProviderName])
	}
}

func TestProviderCallEmbeddingFindsAnEmbeddingsProvider(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(listedFake{name: "openai", emb: &provider.EmbeddingResponse{}})
	mw := middlewares.NewProviderCall(nil, reg)
	req := &pipeline.Request{Type: pipeline.RequestEmbedding, Embedding: &provider.EmbeddingRequest{Model: "e", Input: []string{"x"}}, State: map[string]any{}}
	resp, err := mw.Process(context.Background(), req, nil)
	if err != nil || resp == nil || resp.Embedding == nil {
		t.Fatalf("embedding through the provider call = %+v, %v", resp, err)
	}
	if req.State[pipeline.StateProviderName] != "openai" {
		t.Fatalf("provider name = %v", req.State[pipeline.StateProviderName])
	}
}

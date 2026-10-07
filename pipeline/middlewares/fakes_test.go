package middlewares_test

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// listedFake is a provider with a fixed name, model list and answers. Tests
// embed it and override the methods they need.
type listedFake struct {
	name   string
	models []provider.Model
	resp   *provider.CompletionResponse
	emb    *provider.EmbeddingResponse
}

func (f listedFake) Name() string { return f.name }
func (f listedFake) Capabilities() provider.Capabilities {
	return provider.Capabilities{Chat: true, Streaming: true, Embeddings: true}
}
func (f listedFake) Models(context.Context) ([]provider.Model, error) { return f.models, nil }
func (f listedFake) Complete(context.Context, *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return f.resp, nil
}
func (f listedFake) CompleteStream(context.Context, *provider.CompletionRequest) (provider.Stream, error) {
	return nil, nil //nolint:nilnil // not used by these tests
}
func (f listedFake) Embed(context.Context, *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return f.emb, nil
}
func (f listedFake) Healthy(context.Context) bool { return true }

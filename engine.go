package nexus

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// Engine processes AI requests without any HTTP concerns.
// It delegates to the pipeline for the actual processing.
type Engine struct {
	gw *Gateway
}

func newEngine(gw *Gateway) *Engine {
	return &Engine{gw: gw}
}

// NewEngine creates a standalone engine (no HTTP).
func NewEngine(opts ...Option) *Engine {
	gw := New(opts...)
	_ = gw.Initialize(context.Background())
	return gw.engine
}

// Complete sends a chat completion request through the full pipeline.
func (e *Engine) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	if e.gw.pipeline == nil {
		return nil, ErrProviderNotFound
	}
	return e.gw.pipeline.Execute(ctx, req)
}

// CompleteStream sends a streaming chat completion request.
func (e *Engine) CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	if e.gw.pipeline == nil {
		return nil, ErrProviderNotFound
	}
	return e.gw.pipeline.ExecuteStream(ctx, req)
}

// Embed sends an embedding request.
func (e *Engine) Embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	if e.gw.pipeline == nil {
		return nil, ErrProviderNotFound
	}
	return e.gw.pipeline.ExecuteEmbedding(ctx, req)
}

// ListModels returns available models across all providers.
func (e *Engine) ListModels(ctx context.Context) ([]provider.Model, error) {
	if e.gw.model != nil {
		return e.gw.model.ListModels(ctx)
	}
	// Fallback: aggregate from all providers
	var models []provider.Model
	for _, p := range e.gw.providers.All() {
		m, err := p.Models(ctx)
		if err != nil {
			continue
		}
		models = append(models, m...)
	}
	return models, nil
}

// Gateway returns the underlying Gateway.
func (e *Engine) Gateway() *Gateway { return e.gw }

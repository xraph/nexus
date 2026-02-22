// Package pipeline defines the composable middleware pipeline that processes
// every request through an ordered chain of middleware.
package pipeline

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// Service processes requests through an ordered middleware chain.
type Service interface {
	// Execute processes a completion request through the full pipeline.
	Execute(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error)

	// ExecuteStream processes a streaming request through the pipeline.
	ExecuteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error)

	// ExecuteEmbedding processes an embedding request.
	ExecuteEmbedding(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error)
}

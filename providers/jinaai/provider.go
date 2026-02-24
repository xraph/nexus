// Package jinaai provides a Jina AI provider implementation for Nexus.
// Jina AI uses an OpenAI-compatible API for embeddings and does not support
// chat completions. This provider wraps the OpenAI provider with the Jina AI
// base URL.
package jinaai

import (
	"context"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/openai"
)

const defaultBaseURL = "https://api.jina.ai/v1"

// Provider implements the Nexus provider interface for Jina AI.
type Provider struct {
	inner   *openai.Provider
	baseURL string
}

// New creates a new Jina AI provider.
func New(apiKey string, opts ...Option) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}
	baseURL := defaultBaseURL
	if p.baseURL != "" {
		baseURL = p.baseURL
	}
	p.inner = openai.New(apiKey, openai.WithBaseURL(baseURL))
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "jinaai" }

// Capabilities returns what this provider supports.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Embeddings: true,
	}
}

// Models returns the list of available models.
func (p *Provider) Models(_ context.Context) ([]provider.Model, error) {
	return jinaAIModels(), nil
}

// Complete is not supported by Jina AI.
func (p *Provider) Complete(_ context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, provider.ErrNotSupported
}

// CompleteStream is not supported by Jina AI.
func (p *Provider) CompleteStream(_ context.Context, _ *provider.CompletionRequest) (provider.Stream, error) {
	return nil, provider.ErrNotSupported
}

// Embed sends an embedding request via the inner OpenAI-compatible client.
func (p *Provider) Embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	resp, err := p.inner.Embed(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.Provider = "jinaai"
	return resp, nil
}

// Healthy returns true if the provider is reachable.
func (p *Provider) Healthy(ctx context.Context) bool {
	return p.inner.Healthy(ctx)
}

// Option configures the Jina AI provider.
type Option func(*Provider)

// WithBaseURL sets a custom API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

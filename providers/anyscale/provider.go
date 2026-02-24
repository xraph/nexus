// Package anyscale provides an Anyscale provider implementation for Nexus.
// Anyscale provides Ray-based distributed inference.
package anyscale

import (
	"context"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/openai"
)

const defaultBaseURL = "https://api.endpoints.anyscale.com/v1"

// Provider implements the Nexus provider interface for Anyscale.
type Provider struct {
	inner   *openai.Provider
	baseURL string
	models  []provider.Model
}

// New creates a new Anyscale provider.
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
	p.models = anyscaleModels()
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "anyscale" }

// Capabilities returns what this provider supports.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:      true,
		Streaming: true,
		JSON:      true,
	}
}

// Models returns the list of available models.
func (p *Provider) Models(_ context.Context) ([]provider.Model, error) {
	return p.models, nil
}

// Complete sends a chat completion request.
func (p *Provider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	resp, err := p.inner.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.Provider = "anyscale"
	return resp, nil
}

// CompleteStream sends a streaming chat completion request.
func (p *Provider) CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	return p.inner.CompleteStream(ctx, req)
}

// Embed sends an embedding request — Anyscale does not support embeddings.
func (p *Provider) Embed(_ context.Context, _ *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, provider.ErrNotSupported
}

// Healthy returns true if the provider is reachable.
func (p *Provider) Healthy(ctx context.Context) bool {
	return p.inner.Healthy(ctx)
}

// Option configures the Anyscale provider.
type Option func(*Provider)

// WithBaseURL sets a custom API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

// Package voyageai provides a Voyage AI provider implementation for Nexus.
// Voyage AI specializes in high-quality text embeddings and does not support
// chat completions.
package voyageai

import (
	"context"

	"github.com/xraph/nexus/provider"
)

const defaultBaseURL = "https://api.voyageai.com"

// Provider implements the Nexus provider interface for Voyage AI.
type Provider struct {
	apiKey  string
	baseURL string
	client  *client
}

// New creates a new Voyage AI provider.
func New(apiKey string, opts ...Option) *Provider {
	p := &Provider{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
	}
	for _, opt := range opts {
		opt(p)
	}
	p.client = newClient(p.apiKey, p.baseURL)
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "voyageai" }

// Capabilities returns what this provider supports.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Embeddings: true,
	}
}

// Models returns the list of available models.
func (p *Provider) Models(_ context.Context) ([]provider.Model, error) {
	return voyageAIModels(), nil
}

// Complete is not supported by Voyage AI.
func (p *Provider) Complete(_ context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, provider.ErrNotSupported
}

// CompleteStream is not supported by Voyage AI.
func (p *Provider) CompleteStream(_ context.Context, _ *provider.CompletionRequest) (provider.Stream, error) {
	return nil, provider.ErrNotSupported
}

// Embed sends an embedding request.
func (p *Provider) Embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return p.client.embed(ctx, req)
}

// Healthy returns true if the provider is reachable.
func (p *Provider) Healthy(ctx context.Context) bool {
	return p.client.ping(ctx) == nil
}

// Option configures the Voyage AI provider.
type Option func(*Provider)

// WithBaseURL sets a custom API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

// Package anthropic provides an Anthropic provider implementation for Nexus.
package anthropic

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// Provider implements the Nexus provider interface for Anthropic.
type Provider struct {
	apiKey  string
	baseURL string
	client  *client
}

// New creates a new Anthropic provider.
func New(apiKey string, opts ...Option) *Provider {
	p := &Provider{
		apiKey:  apiKey,
		baseURL: "https://api.anthropic.com",
	}
	for _, opt := range opts {
		opt(p)
	}
	p.client = newClient(p.apiKey, p.baseURL)
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "anthropic" }

// Capabilities returns what this provider supports.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:      true,
		Streaming: true,
		Vision:    true,
		Tools:     true,
		JSON:      true,
		Thinking:  true, // extended thinking
		Batch:     true,
	}
}

// Models returns the list of available models.
func (p *Provider) Models(_ context.Context) ([]provider.Model, error) {
	return anthropicModels(), nil
}

// Complete sends a chat completion request.
func (p *Provider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return p.client.complete(ctx, req)
}

// CompleteStream sends a streaming chat completion request.
func (p *Provider) CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	return p.client.completeStream(ctx, req)
}

// Embed sends an embedding request — Anthropic does not support embeddings.
func (p *Provider) Embed(_ context.Context, _ *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, provider.ErrNotSupported
}

// Healthy returns true if the provider is reachable.
func (p *Provider) Healthy(ctx context.Context) bool {
	return p.client.ping(ctx) == nil
}

// Option configures the Anthropic provider.
type Option func(*Provider)

// WithBaseURL sets a custom API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

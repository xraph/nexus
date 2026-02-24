// Package deepseek provides a DeepSeek provider implementation for Nexus.
// DeepSeek offers reasoning-capable models including DeepSeek-R1.
package deepseek

import (
	"context"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/openai"
)

const defaultBaseURL = "https://api.deepseek.com"

// Provider implements the Nexus provider interface for DeepSeek.
type Provider struct {
	inner   *openai.Provider
	baseURL string
	models  []provider.Model
}

// New creates a new DeepSeek provider.
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
	p.models = deepseekModels()
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "deepseek" }

// Capabilities returns what this provider supports.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:      true,
		Streaming: true,
		Tools:     true,
		JSON:      true,
		Thinking:  true,
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
	resp.Provider = "deepseek"
	return resp, nil
}

// CompleteStream sends a streaming chat completion request.
func (p *Provider) CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	return p.inner.CompleteStream(ctx, req)
}

// Embed sends an embedding request — DeepSeek does not support embeddings.
func (p *Provider) Embed(_ context.Context, _ *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, provider.ErrNotSupported
}

// Healthy returns true if the provider is reachable.
func (p *Provider) Healthy(ctx context.Context) bool {
	return p.inner.Healthy(ctx)
}

// Option configures the DeepSeek provider.
type Option func(*Provider)

// WithBaseURL sets a custom API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

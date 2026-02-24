// Package ollama provides an Ollama provider implementation for Nexus.
// Ollama runs models locally and exposes an OpenAI-compatible API.
// No API key is required by default.
package ollama

import (
	"context"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/openai"
)

const defaultBaseURL = "http://localhost:11434/v1"

// Provider implements the Nexus provider interface for Ollama.
type Provider struct {
	inner   *openai.Provider
	baseURL string
	models  []provider.Model
}

// New creates a new Ollama provider.
// API key can be empty since Ollama doesn't require authentication by default.
func New(opts ...Option) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}
	baseURL := defaultBaseURL
	if p.baseURL != "" {
		baseURL = p.baseURL
	}
	// Ollama doesn't require an API key; pass empty string.
	p.inner = openai.New("", openai.WithBaseURL(baseURL))
	p.models = ollamaModels()
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "ollama" }

// Capabilities returns what this provider supports.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:       true,
		Streaming:  true,
		Embeddings: true,
		Vision:     true,
		Tools:      true,
		JSON:       true,
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
	resp.Provider = "ollama"
	return resp, nil
}

// CompleteStream sends a streaming chat completion request.
func (p *Provider) CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	return p.inner.CompleteStream(ctx, req)
}

// Embed sends an embedding request.
func (p *Provider) Embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	resp, err := p.inner.Embed(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.Provider = "ollama"
	return resp, nil
}

// Healthy returns true if the provider is reachable.
func (p *Provider) Healthy(ctx context.Context) bool {
	return p.inner.Healthy(ctx)
}

// Option configures the Ollama provider.
type Option func(*Provider)

// WithBaseURL sets a custom API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

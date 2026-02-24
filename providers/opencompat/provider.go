// Package opencompat provides a generic OpenAI-compatible provider implementation.
// Use this for any provider that exposes an OpenAI-compatible API (e.g., Together,
// Groq, Fireworks, local vLLM, etc.)
package opencompat

import (
	"context"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/openai"
)

// Provider wraps the OpenAI provider with a custom name and base URL.
type Provider struct {
	name   string
	inner  *openai.Provider
	caps   provider.Capabilities
	models []provider.Model
}

// New creates a generic OpenAI-compatible provider.
func New(name, baseURL, apiKey string, opts ...Option) *Provider {
	p := &Provider{
		name:  name,
		inner: openai.New(apiKey, openai.WithBaseURL(baseURL)),
		caps: provider.Capabilities{
			Chat:      true,
			Streaming: true,
		},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return p.name }

// Capabilities returns what this provider supports.
func (p *Provider) Capabilities() provider.Capabilities { return p.caps }

// Models returns the configured model list.
func (p *Provider) Models(_ context.Context) ([]provider.Model, error) {
	return p.models, nil
}

// Complete sends a chat completion request.
func (p *Provider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	resp, err := p.inner.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.Provider = p.name
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
	resp.Provider = p.name
	return resp, nil
}

// Healthy returns true if the provider is reachable.
func (p *Provider) Healthy(ctx context.Context) bool {
	return p.inner.Healthy(ctx)
}

// Option configures the OpenCompat provider.
type Option func(*Provider)

// WithCapabilities sets the capabilities.
func WithCapabilities(caps provider.Capabilities) Option {
	return func(p *Provider) { p.caps = caps }
}

// WithModels sets the model list.
func WithModels(models []provider.Model) Option {
	return func(p *Provider) { p.models = models }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

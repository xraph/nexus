// Package gemini provides a Google Gemini provider implementation for Nexus.
// Gemini uses a different API format from OpenAI (contents[], generateContent endpoint,
// API key in query param).
package gemini

import (
	"context"

	"github.com/xraph/nexus/provider"
)

const defaultBaseURL = "https://generativelanguage.googleapis.com"

// Provider implements the Nexus provider interface for Google Gemini.
type Provider struct {
	apiKey  string
	baseURL string
	client  *client
}

// New creates a new Gemini provider.
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
func (p *Provider) Name() string { return "gemini" }

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
func (p *Provider) Models(ctx context.Context) ([]provider.Model, error) {
	return geminiModels(), nil
}

// Complete sends a chat completion request.
func (p *Provider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return p.client.complete(ctx, req)
}

// CompleteStream sends a streaming chat completion request.
func (p *Provider) CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	return p.client.completeStream(ctx, req)
}

// Embed sends an embedding request.
func (p *Provider) Embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return p.client.embed(ctx, req)
}

// Healthy returns true if the provider is reachable.
func (p *Provider) Healthy(ctx context.Context) bool {
	return p.client.ping(ctx) == nil
}

// Option configures the Gemini provider.
type Option func(*Provider)

// WithBaseURL sets a custom API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

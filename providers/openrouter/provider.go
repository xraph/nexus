// Package openrouter provides an OpenRouter provider implementation for Nexus.
// OpenRouter is a meta-router that gives access to many models via a single API,
// with extra headers for HTTP-Referer and X-Title.
package openrouter

import (
	"context"
	"net/http"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/openai"
)

const defaultBaseURL = "https://openrouter.ai/api/v1"

// Provider implements the Nexus provider interface for OpenRouter.
type Provider struct {
	inner    *openai.Provider
	baseURL  string
	siteURL  string
	siteName string
	models   []provider.Model
}

// New creates a new OpenRouter provider.
func New(apiKey string, opts ...Option) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}
	baseURL := defaultBaseURL
	if p.baseURL != "" {
		baseURL = p.baseURL
	}

	// OpenRouter requires extra headers, so we use a custom HTTP transport.
	openaiOpts := []openai.Option{openai.WithBaseURL(baseURL)}
	p.inner = openai.New(apiKey, openaiOpts...)
	p.models = openRouterModels()
	return p
}

// headerTransport adds OpenRouter-specific headers to every request.
type headerTransport struct {
	base     http.RoundTripper
	siteURL  string
	siteName string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.siteURL != "" {
		req.Header.Set("HTTP-Referer", t.siteURL)
	}
	if t.siteName != "" {
		req.Header.Set("X-Title", t.siteName)
	}
	return t.base.RoundTrip(req)
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "openrouter" }

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
	return p.models, nil
}

// Complete sends a chat completion request.
func (p *Provider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	resp, err := p.inner.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	resp.Provider = "openrouter"
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
	resp.Provider = "openrouter"
	return resp, nil
}

// Healthy returns true if the provider is reachable.
func (p *Provider) Healthy(ctx context.Context) bool {
	return p.inner.Healthy(ctx)
}

// Option configures the OpenRouter provider.
type Option func(*Provider)

// WithBaseURL sets a custom API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// WithSiteURL sets the HTTP-Referer header for OpenRouter rankings.
func WithSiteURL(url string) Option {
	return func(p *Provider) { p.siteURL = url }
}

// WithSiteName sets the X-Title header for OpenRouter rankings.
func WithSiteName(name string) Option {
	return func(p *Provider) { p.siteName = name }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

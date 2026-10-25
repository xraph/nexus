// Package anthropic provides an Anthropic provider implementation for Nexus.
package anthropic

import (
	"context"
	"net/http"
	"time"

	"github.com/xraph/nexus/provider"
)

// Anthropic API versions, sent as the anthropic-version header.
const (
	// APIVersion20230601 is the default version.
	APIVersion20230601 = "2023-06-01"
	// APIVersion20261021 rejects temperature, top_p and top_k on every
	// model, so the provider drops them from every request.
	APIVersion20261021 = "2026-10-21"
)

// defaultTimeout bounds the wait for response headers, matching the
// Anthropic SDKs' default request timeout.
const defaultTimeout = 10 * time.Minute

// Provider implements the Nexus provider interface for Anthropic.
type Provider struct {
	apiKey     string
	baseURL    string
	apiVersion string
	timeout    time.Duration
	client     *client
}

// New creates a new Anthropic provider.
func New(apiKey string, opts ...Option) *Provider {
	p := &Provider{
		apiKey:     apiKey,
		baseURL:    "https://api.anthropic.com",
		apiVersion: APIVersion20230601,
		timeout:    defaultTimeout,
	}
	for _, opt := range opts {
		opt(p)
	}
	p.client = newClient(p.apiKey, p.baseURL, p.apiVersion, p.timeout)
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "anthropic" }

// Capabilities returns what this provider supports.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:               true,
		Streaming:          true,
		Vision:             true,
		Tools:              true,
		JSON:               true,
		Thinking:           true, // extended thinking
		Batch:              true,
		StreamingReasoning: true, // thinking_delta on claude-3.7+
		StreamingTools:     true, // input_json_delta accumulator
		StreamingCitations: true, // citations_delta
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

// WithAPIVersion sets the anthropic-version header. The default is
// APIVersion20230601.
func WithAPIVersion(v string) Option {
	return func(p *Provider) {
		if v != "" {
			p.apiVersion = v
		}
	}
}

// WithTimeout bounds how long a request waits for response headers. The
// default is 10 minutes; a zero or negative d keeps it. Once headers arrive
// a stream is not cut by it, however long it runs: bound the whole call with
// the request context instead.
func WithTimeout(d time.Duration) Option {
	return func(p *Provider) {
		if d > 0 {
			p.timeout = d
		}
	}
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

// WrapTransport wraps the transport of the HTTP client the provider sends its
// requests through (provider.TransportWrapper). Nexus calls it once, from
// Initialize, with the wrapper given to nexus.WithProviderTransport. Call it
// before the first request, never during one.
func (p *Provider) WrapTransport(wrap func(http.RoundTripper) http.RoundTripper) {
	if rt := wrap(p.client.http.Transport); rt != nil {
		p.client.http.Transport = rt
	}
}

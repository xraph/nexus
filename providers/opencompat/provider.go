// Package opencompat provides a generic OpenAI-compatible provider implementation.
// Use this for any provider that exposes an OpenAI-compatible API (e.g., Together,
// Groq, Fireworks, local vLLM, etc.)
package opencompat

import (
	"context"
	"net/http"
	"time"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/openai"
)

// Provider wraps the OpenAI provider with a custom name and base URL.
type Provider struct {
	name      string
	inner     *openai.Provider
	innerOpts []openai.Option
	caps      provider.Capabilities
	models    []provider.Model
}

// New creates a generic OpenAI-compatible provider. baseURL and apiKey may
// be "" when every request brings a tenant provider.Credential: a request
// whose context carries one is sent with its key and, when it sets one,
// base URL. A request with no base URL from either fails with
// provider.ErrMissingBaseURL before any network call.
//
// The default capabilities are chat, streaming, tools (streamed too), JSON
// output and embeddings, which most OpenAI-compatible servers support;
// WithCapabilities replaces them.
func New(name, baseURL, apiKey string, opts ...Option) *Provider {
	p := &Provider{
		name: name,
		caps: provider.Capabilities{
			Chat:           true,
			Streaming:      true,
			Embeddings:     true,
			Tools:          true,
			JSON:           true,
			StreamingTools: true,
		},
	}
	for _, opt := range opts {
		opt(p)
	}
	p.inner = openai.New(apiKey, append([]openai.Option{openai.WithBaseURL(baseURL)}, p.innerOpts...)...)
	return p
}

// AcceptsCredentials reports that the provider applies a per-request
// provider.Credential (provider.CredentialAware).
func (p *Provider) AcceptsCredentials() bool { return true }

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

// WithTimeout bounds how long a request waits for response headers. The
// default is 10 minutes; a zero or negative d keeps it. Once headers arrive
// a stream is not cut by it: bound the whole call with the request context.
func WithTimeout(d time.Duration) Option {
	return func(p *Provider) { p.innerOpts = append(p.innerOpts, openai.WithTimeout(d)) }
}

// WithModels sets the model list.
func WithModels(models []provider.Model) Option {
	return func(p *Provider) { p.models = models }
}

// Compile-time checks.
var (
	_ provider.Provider        = (*Provider)(nil)
	_ provider.CredentialAware = (*Provider)(nil)
)

// WrapTransport wraps the transport of the HTTP client the provider sends its
// requests through (provider.TransportWrapper). Nexus calls it once, from
// Initialize, with the wrapper given to nexus.WithProviderTransport. Call it
// before the first request, never during one.
func (p *Provider) WrapTransport(wrap func(http.RoundTripper) http.RoundTripper) {
	p.inner.WrapTransport(wrap)
}

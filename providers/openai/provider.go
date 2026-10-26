// Package openai provides an OpenAI provider implementation for Nexus.
package openai

import (
	"context"
	"net/http"
	"time"

	"github.com/xraph/nexus/provider"
)

// defaultTimeout bounds the wait for response headers, matching the OpenAI
// SDKs' default request timeout.
const defaultTimeout = 10 * time.Minute

// Provider implements the Nexus provider interface for OpenAI.
type Provider struct {
	apiKey  string
	baseURL string
	orgID   string
	timeout time.Duration
	client  *client

	maxCompletionTokens bool
}

// New creates a new OpenAI provider. A request whose context carries a
// provider.Credential is sent with that credential's key and, when it sets
// one, base URL instead of the configured ones.
func New(apiKey string, opts ...Option) *Provider {
	p := &Provider{
		apiKey:  apiKey,
		baseURL: officialBaseURL,
		timeout: defaultTimeout,
	}
	for _, opt := range opts {
		opt(p)
	}
	p.client = newClient(p.apiKey, p.baseURL, p.orgID, p.timeout)
	p.client.maxCompletionTokens = p.maxCompletionTokens
	return p
}

// AcceptsCredentials reports that the provider applies a per-request
// provider.Credential (provider.CredentialAware).
func (p *Provider) AcceptsCredentials() bool { return true }

// Name returns the provider identifier.
func (p *Provider) Name() string { return "openai" }

// Capabilities returns what this provider supports.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:               true,
		Streaming:          true,
		Embeddings:         true,
		Vision:             true,
		Tools:              true,
		JSON:               true,
		Images:             true,
		Thinking:           true, // o-series
		Batch:              true,
		StreamingReasoning: true, // o-series + Responses API
		StreamingTools:     true, // tool_call deltas with index
		StreamingAudio:     true, // gpt-4o-audio + Realtime
	}
}

// Models returns the list of available models.
func (p *Provider) Models(_ context.Context) ([]provider.Model, error) {
	return openAIModels(), nil
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

// SupportsDimensions reports whether an embedding of model can ask for a
// width (provider.DimensionsAware). On OpenAI's own endpoint only the
// text-embedding-3 models can. On any other base URL the endpoint decides,
// so it answers true and the gateway checks the length of what comes back.
// It judges by the configured base URL; a tenant credential's base URL is
// not known until the call.
func (p *Provider) SupportsDimensions(model string) bool {
	if !isOfficial(p.baseURL) {
		return true
	}
	t, _ := modelTraitsFor(model)
	return t.embeddingDimensions
}

// Healthy returns true if the provider is reachable.
func (p *Provider) Healthy(ctx context.Context) bool {
	return p.client.ping(ctx) == nil
}

// Option configures the OpenAI provider.
type Option func(*Provider)

// WithBaseURL sets a custom API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
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

// WithMaxCompletionTokens sends the output cap as max_completion_tokens on
// any base URL. OpenAI's own endpoint (https://api.openai.com/v1) always
// gets max_completion_tokens; other base URLs get max_tokens, which most
// OpenAI-compatible servers expect, unless this is on.
func WithMaxCompletionTokens(on bool) Option {
	return func(p *Provider) { p.maxCompletionTokens = on }
}

// WithOrgID sets the OpenAI organization ID.
func WithOrgID(orgID string) Option {
	return func(p *Provider) { p.orgID = orgID }
}

// Compile-time checks.
var (
	_ provider.Provider        = (*Provider)(nil)
	_ provider.CredentialAware = (*Provider)(nil)
	_ provider.DimensionsAware = (*Provider)(nil)
)

// WrapTransport wraps the transport of the HTTP client the provider sends its
// requests through (provider.TransportWrapper). Nexus calls it once, from
// Initialize, with the wrapper given to nexus.WithProviderTransport. Call it
// before the first request, never during one.
func (p *Provider) WrapTransport(wrap func(http.RoundTripper) http.RoundTripper) {
	if rt := wrap(p.client.http.Transport); rt != nil {
		p.client.http.Transport = rt
	}
}

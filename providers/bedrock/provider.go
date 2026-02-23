// Package bedrock provides an Amazon Bedrock provider implementation for Nexus.
package bedrock

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// Provider implements the Nexus provider interface for Amazon Bedrock.
type Provider struct {
	accessKeyID     string
	secretAccessKey string
	sessionToken    string
	region          string
	baseURL         string
	client          *client
}

// New creates a new Bedrock provider.
func New(accessKeyID, secretAccessKey, region string, opts ...Option) *Provider {
	p := &Provider{
		accessKeyID:     accessKeyID,
		secretAccessKey: secretAccessKey,
		region:          region,
	}
	for _, opt := range opts {
		opt(p)
	}
	p.client = newClient(p.accessKeyID, p.secretAccessKey, p.sessionToken, p.region, p.baseURL)
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "bedrock" }

// Capabilities returns what this provider supports.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:      true,
		Streaming: true,
		Tools:     true,
		JSON:      true,
	}
}

// Models returns the list of available models.
func (p *Provider) Models(ctx context.Context) ([]provider.Model, error) {
	return bedrockModels(), nil
}

// Complete sends a chat completion request.
func (p *Provider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return p.client.complete(ctx, req)
}

// CompleteStream sends a streaming chat completion request.
func (p *Provider) CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	return p.client.completeStream(ctx, req)
}

// Embed sends an embedding request -- Bedrock via Converse API does not support embeddings.
func (p *Provider) Embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, provider.ErrNotSupported
}

// Healthy returns true if the provider is reachable.
func (p *Provider) Healthy(ctx context.Context) bool {
	return p.client.ping(ctx) == nil
}

// Option configures the Bedrock provider.
type Option func(*Provider)

// WithBaseURL sets a custom Bedrock runtime endpoint URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// WithSessionToken sets an AWS session token for temporary credentials.
func WithSessionToken(token string) Option {
	return func(p *Provider) { p.sessionToken = token }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

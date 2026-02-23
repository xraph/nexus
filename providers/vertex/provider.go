// Package vertex provides a Google Vertex AI provider implementation for Nexus.
// Vertex AI uses the same Gemini API format but with different authentication
// (OAuth2 Bearer tokens) and URL structure (project/location based).
package vertex

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// Provider implements the Nexus provider interface for Google Vertex AI.
type Provider struct {
	projectID       string
	location        string
	accessToken     string
	credentialsJSON []byte
	baseURL         string
	client          *client
}

// New creates a new Vertex AI provider.
func New(opts ...Option) *Provider {
	p := &Provider{
		location: "us-central1",
	}
	for _, opt := range opts {
		opt(p)
	}
	p.client = newClient(p.projectID, p.location, p.accessToken, p.credentialsJSON, p.baseURL)
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "vertex" }

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
	return vertexModels(), nil
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

// Option configures the Vertex AI provider.
type Option func(*Provider)

// WithProjectID sets the Google Cloud project ID.
func WithProjectID(id string) Option {
	return func(p *Provider) { p.projectID = id }
}

// WithLocation sets the Google Cloud location (e.g., "us-central1").
func WithLocation(loc string) Option {
	return func(p *Provider) { p.location = loc }
}

// WithAccessToken sets a static access token for authentication.
func WithAccessToken(token string) Option {
	return func(p *Provider) { p.accessToken = token }
}

// WithCredentialsJSON sets service account JSON credentials for automatic
// OAuth2 token management.
func WithCredentialsJSON(json []byte) Option {
	return func(p *Provider) { p.credentialsJSON = json }
}

// WithBaseURL sets a custom API base URL, overriding the location-based URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

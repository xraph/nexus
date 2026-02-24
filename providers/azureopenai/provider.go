// Package azureopenai provides an Azure OpenAI provider implementation for Nexus.
// Azure OpenAI uses the same request/response format as OpenAI but with a different
// URL structure and authentication header.
package azureopenai

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// Provider implements the Nexus provider interface for Azure OpenAI.
type Provider struct {
	apiKey       string
	resourceName string
	deploymentID string
	apiVersion   string
	baseURL      string
	client       *client
}

// New creates a new Azure OpenAI provider.
func New(apiKey string, opts ...Option) *Provider {
	p := &Provider{
		apiKey:     apiKey,
		apiVersion: "2024-08-01-preview",
	}
	for _, opt := range opts {
		opt(p)
	}
	p.client = newClient(p.apiKey, p.resourceName, p.deploymentID, p.apiVersion, p.baseURL)
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "azureopenai" }

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
	return azureOpenAIModels(), nil
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

// Option configures the Azure OpenAI provider.
type Option func(*Provider)

// WithResourceName sets the Azure resource name.
func WithResourceName(name string) Option {
	return func(p *Provider) { p.resourceName = name }
}

// WithDeploymentID sets the Azure deployment ID.
func WithDeploymentID(id string) Option {
	return func(p *Provider) { p.deploymentID = id }
}

// WithAPIVersion sets the Azure OpenAI API version.
func WithAPIVersion(version string) Option {
	return func(p *Provider) { p.apiVersion = version }
}

// WithBaseURL sets a custom API base URL, overriding the resource name based URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

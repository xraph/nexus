// Package openairealtime implements the Nexus provider interface against
// OpenAI's Realtime API (wss://api.openai.com/v1/realtime). The upstream
// protocol is bidirectional WebSocket with a session-oriented event model;
// this package translates the OpenAI event vocabulary into nexus
// StreamChunk frames so callers can consume realtime audio and text using
// the same Stream / BiStream contracts as any other provider.
package openairealtime

import (
	"context"
	"errors"

	"github.com/xraph/nexus/provider"
)

// Provider implements provider.Provider against OpenAI Realtime.
type Provider struct {
	apiKey  string
	baseURL string
	model   string

	// dial lets tests inject a fake WebSocket dialer that doesn't hit the
	// network. Production code calls dialDefault.
	dial dialFunc
}

// Option configures the Realtime provider.
type Option func(*Provider)

// WithBaseURL overrides the upstream WebSocket URL (testing/staging).
func WithBaseURL(u string) Option { return func(p *Provider) { p.baseURL = u } }

// WithModel sets the Realtime session's default model.
func WithModel(m string) Option { return func(p *Provider) { p.model = m } }

// withDialer is internal — used by tests to inject a fake transport.
func withDialer(d dialFunc) Option { return func(p *Provider) { p.dial = d } }

// New returns a Realtime provider backed by the given API key.
func New(apiKey string, opts ...Option) *Provider {
	p := &Provider{
		apiKey:  apiKey,
		baseURL: "wss://api.openai.com/v1/realtime",
		model:   "gpt-4o-realtime-preview",
		dial:    dialDefault,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "openai-realtime" }

// Capabilities reports the streaming feature matrix.
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Chat:               true,
		Streaming:          true,
		Tools:              true,
		Audio:              true,
		StreamingReasoning: false,
		StreamingTools:     true,
		StreamingAudio:     true,
		RealtimeAudio:      true,
		LiveBidi:           true,
	}
}

// Models returns the supported Realtime models. The list is small and
// hand-maintained — call OpenAI's /v1/models endpoint via the regular
// openai package for the canonical list.
func (p *Provider) Models(_ context.Context) ([]provider.Model, error) {
	return []provider.Model{
		{Provider: p.Name(), Name: "gpt-4o-realtime-preview"},
		{Provider: p.Name(), Name: "gpt-4o-mini-realtime-preview"},
	}, nil
}

// Complete is not supported on Realtime — the API is streaming-only.
func (p *Provider) Complete(_ context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, errors.New("openairealtime: non-streaming Complete is not supported; use CompleteStream")
}

// CompleteStream opens a Realtime session and returns a BiStream.
func (p *Provider) CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	conn, err := p.dial(ctx, p.baseURL, p.apiKey, model)
	if err != nil {
		return nil, err
	}
	return newRealtimeStream(ctx, conn, model, req), nil
}

// Embed is not supported on Realtime.
func (p *Provider) Embed(_ context.Context, _ *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, errors.New("openairealtime: embeddings are not supported on Realtime")
}

// Healthy reports whether the upstream is reachable. We don't open a real
// session here — Realtime sessions are billable — so we return true and
// rely on CompleteStream to surface connection errors.
func (p *Provider) Healthy(_ context.Context) bool { return true }

// Compile-time check.
var _ provider.Provider = (*Provider)(nil)

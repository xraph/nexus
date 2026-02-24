// Package middlewares provides built-in pipeline middleware for Nexus.
package middlewares

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
)

// ProviderCallMiddleware is the core middleware that routes to a provider and
// executes the request. It sits at priority 350 (middle of the routing range).
type ProviderCallMiddleware struct {
	router    router.Service
	providers provider.Registry
}

// NewProviderCall creates the core provider-calling middleware.
func NewProviderCall(r router.Service, providers provider.Registry) *ProviderCallMiddleware {
	return &ProviderCallMiddleware{
		router:    r,
		providers: providers,
	}
}

func (m *ProviderCallMiddleware) Name() string  { return "provider_call" }
func (m *ProviderCallMiddleware) Priority() int { return 350 }

func (m *ProviderCallMiddleware) Process(ctx context.Context, req *pipeline.Request, _ pipeline.NextFunc) (*pipeline.Response, error) {
	switch req.Type {
	case pipeline.RequestCompletion:
		return m.handleCompletion(ctx, req)
	case pipeline.RequestStream:
		return m.handleStream(ctx, req)
	case pipeline.RequestEmbedding:
		return m.handleEmbedding(ctx, req)
	default:
		return nil, fmt.Errorf("nexus: unknown request type: %s", req.Type)
	}
}

func (m *ProviderCallMiddleware) handleCompletion(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
	p, err := m.selectProvider(ctx, req)
	if err != nil {
		return nil, err
	}

	ctx = pipeline.WithProviderName(ctx, p.Name())
	start := time.Now()

	resp, err := p.Complete(ctx, req.Completion)
	if err != nil {
		return nil, fmt.Errorf("nexus: provider %s: %w", p.Name(), err)
	}

	// Store timing
	req.State["provider_latency"] = time.Since(start)
	req.State["provider_name"] = p.Name()

	return &pipeline.Response{Completion: resp}, nil
}

func (m *ProviderCallMiddleware) handleStream(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
	p, err := m.selectProvider(ctx, req)
	if err != nil {
		return nil, err
	}

	ctx = pipeline.WithProviderName(ctx, p.Name())
	req.State["provider_name"] = p.Name()

	stream, err := p.CompleteStream(ctx, req.Completion)
	if err != nil {
		return nil, fmt.Errorf("nexus: provider %s stream: %w", p.Name(), err)
	}

	return &pipeline.Response{Stream: stream}, nil
}

func (m *ProviderCallMiddleware) handleEmbedding(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
	if req.Embedding == nil {
		return nil, errors.New("nexus: embedding request is nil")
	}

	// For embeddings, pick the first provider that supports embeddings
	allProviders := m.providers.WithCapability("embed")
	if len(allProviders) == 0 {
		return nil, errors.New("nexus: no providers support embeddings")
	}

	p := allProviders[0]
	ctx = pipeline.WithProviderName(ctx, p.Name())
	req.State["provider_name"] = p.Name()

	resp, err := p.Embed(ctx, req.Embedding)
	if err != nil {
		return nil, fmt.Errorf("nexus: provider %s embed: %w", p.Name(), err)
	}

	return &pipeline.Response{Embedding: resp}, nil
}

func (m *ProviderCallMiddleware) selectProvider(ctx context.Context, req *pipeline.Request) (provider.Provider, error) {
	allProviders := m.providers.All()
	if len(allProviders) == 0 {
		return nil, errors.New("nexus: no providers registered")
	}

	if m.router != nil {
		p, err := m.router.Route(ctx, req.Completion, allProviders)
		if err != nil {
			return nil, fmt.Errorf("nexus: routing: %w", err)
		}
		return p, nil
	}

	// No router configured — use first available provider
	return allProviders[0], nil
}

// Package middlewares provides built-in pipeline middleware for Nexus.
package middlewares

import (
	"context"
	"errors"
	"fmt"
	"time"

	gometrics "github.com/xraph/go-utils/metrics"

	"github.com/xraph/nexus/internal/telemetry"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
)

// ProviderCallMiddleware is the core middleware that routes to a provider and
// executes the request. It is the pipeline's terminal stage and always runs last.
//
// Every call to a provider gets a client span that follows the OpenTelemetry
// GenAI conventions: "chat {model}" or "embeddings {model}", with
// gen_ai.provider.name, the request model, the response model and id, finish
// reasons and token counts. A stream's span ends with the stream. The model
// is named only when the provider lists it (see ModelCatalog).
type ProviderCallMiddleware struct {
	router    router.Service
	providers provider.Registry
	catalog   ModelCatalog
	metrics   *genAIMetrics
}

// NewProviderCall creates the core provider-calling middleware. Its model
// catalog is a price book over providers; WithModelCatalog shares another.
func NewProviderCall(r router.Service, providers provider.Registry) *ProviderCallMiddleware {
	return &ProviderCallMiddleware{
		router:    r,
		providers: providers,
		catalog:   model.NewPriceBook(providers),
	}
}

// WithModelCatalog sets the catalog that decides which model names telemetry
// may report. The gateway passes its price book, so both read one cached list.
func (m *ProviderCallMiddleware) WithModelCatalog(c ModelCatalog) *ProviderCallMiddleware {
	m.catalog = c
	return m
}

// WithMetrics records gen_ai.client.operation.duration,
// gen_ai.client.token.usage and nexus_stream_time_to_first_chunk_seconds on
// mm. A nil mm records nothing; spans need no option.
func (m *ProviderCallMiddleware) WithMetrics(mm gometrics.Metrics) *ProviderCallMiddleware {
	m.metrics = newGenAIMetrics(mm)
	return m
}

func (m *ProviderCallMiddleware) Name() string  { return "provider_call" }
func (m *ProviderCallMiddleware) Priority() int { return 350 }

// Terminal marks the provider call as the end of the chain.
func (m *ProviderCallMiddleware) Terminal() {}

func (m *ProviderCallMiddleware) Process(ctx context.Context, req *pipeline.Request, _ pipeline.NextFunc) (*pipeline.Response, error) {
	switch req.Type {
	case pipeline.RequestCompletion:
		return m.handleCompletion(ctx, req)
	case pipeline.RequestStream:
		return m.handleStream(ctx, req)
	case pipeline.RequestEmbedding:
		return m.handleEmbedding(ctx, req)
	default:
		return nil, pipeline.Permanent(fmt.Errorf("nexus: unknown request type: %s", req.Type))
	}
}

func (m *ProviderCallMiddleware) handleCompletion(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
	p, err := m.selectProvider(ctx, req)
	if err != nil {
		return nil, err
	}

	ctx = pipeline.WithProviderName(ctx, p.Name())
	req.State[pipeline.StateProviderName] = p.Name()
	start := time.Now()

	ctx, call := m.startGenAICall(ctx, telemetry.OperationChat, p, req.Type, req.Completion.Model, requestAttributes(req.Completion)...)
	defer call.endIfPanicking()
	resp, err := p.Complete(ctx, req.Completion)
	call.end(ctx, completionResult(resp), err)
	if err != nil {
		return nil, fmt.Errorf("nexus: provider %s: %w", p.Name(), err)
	}

	// Store timing
	req.State["provider_latency"] = time.Since(start)

	return &pipeline.Response{Completion: resp}, nil
}

func (m *ProviderCallMiddleware) handleStream(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
	p, err := m.selectProvider(ctx, req)
	if err != nil {
		return nil, err
	}

	ctx = pipeline.WithProviderName(ctx, p.Name())
	req.State[pipeline.StateProviderName] = p.Name()

	ctx, call := m.startGenAICall(ctx, telemetry.OperationChat, p, req.Type, req.Completion.Model, requestAttributes(req.Completion)...)
	defer call.endIfPanicking()
	stream, err := p.CompleteStream(ctx, req.Completion)
	if err != nil {
		call.end(ctx, genAIResult{}, err)
		return nil, fmt.Errorf("nexus: provider %s stream: %w", p.Name(), err)
	}
	if stream == nil {
		call.end(ctx, genAIResult{}, nil)
		return &pipeline.Response{}, nil
	}

	return &pipeline.Response{Stream: newGenAIStream(ctx, stream, call)}, nil
}

func (m *ProviderCallMiddleware) handleEmbedding(ctx context.Context, req *pipeline.Request) (*pipeline.Response, error) {
	if req.Embedding == nil {
		return nil, errors.New("nexus: embedding request is nil")
	}

	// For embeddings, pick the first provider that supports embeddings
	allProviders := m.providers.WithCapability("embeddings")
	if len(allProviders) == 0 {
		return nil, pipeline.Permanent(errors.New("nexus: no providers support embeddings"))
	}

	p := allProviders[0]
	ctx = pipeline.WithProviderName(ctx, p.Name())
	req.State[pipeline.StateProviderName] = p.Name()

	ctx, call := m.startGenAICall(ctx, telemetry.OperationEmbeddings, p, req.Type, req.Embedding.Model)
	defer call.endIfPanicking()
	resp, err := p.Embed(ctx, req.Embedding)
	call.end(ctx, embeddingResult(resp), err)
	if err != nil {
		return nil, fmt.Errorf("nexus: provider %s embed: %w", p.Name(), err)
	}

	return &pipeline.Response{Embedding: resp}, nil
}

func (m *ProviderCallMiddleware) selectProvider(ctx context.Context, req *pipeline.Request) (provider.Provider, error) {
	allProviders := m.providers.All()
	if len(allProviders) == 0 {
		return nil, pipeline.Permanent(errors.New("nexus: no providers registered"))
	}

	if m.router != nil {
		p, err := m.router.Route(ctx, req.Completion, allProviders)
		if err != nil {
			return nil, pipeline.Permanent(fmt.Errorf("nexus: routing: %w", err))
		}
		return p, nil
	}

	// No router configured — use first available provider
	return allProviders[0], nil
}

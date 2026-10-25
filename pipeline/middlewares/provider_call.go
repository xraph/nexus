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

// ErrUnknownProvider refuses a request that names a provider, itself or
// through an alias target, that is not registered. It is wrapped with the
// name; match it with errors.Is.
var ErrUnknownProvider error = &pipeline.RefusalError{Code: pipeline.CodeInvalidRequest, Status: 400, Message: "unknown provider"}

// ProviderCallMiddleware is the core middleware that routes to a provider and
// executes the request. It is the pipeline's terminal stage and always runs last.
//
// A completion or stream goes to the provider the request names, else to the
// provider of the alias target the alias stage chose, else to the providers
// whose catalog lists the model (the router choosing among them), else to the
// router's choice over every provider. An embedding goes to the provider the
// request names, else to the embeddings providers that list the model (the
// router choosing among them), else to the first embeddings provider. A named
// provider is never swapped for another: one that is not registered is
// refused with ErrUnknownProvider, and one that cannot embed is refused as an
// invalid request wrapping provider.ErrNotSupported.
//
// Every call to a provider gets a client span that follows the OpenTelemetry
// GenAI conventions: "chat {model}" or "embeddings {model}", with
// gen_ai.provider.name, the request model, the response model and id, finish
// reasons and token counts. A stream's span ends with the stream. The model
// is named only when the provider lists it (see ModelCatalog).
//
// With an unpriced policy (WithUnpricedPolicy), a request for a model with
// no price at the chosen provider is put to the policy before the provider
// is called.
type ProviderCallMiddleware struct {
	router    router.Service
	providers provider.Registry
	catalog   ModelCatalog
	metrics   *genAIMetrics

	// prices and unpriced are the unpriced-model policy (WithUnpricedPolicy).
	prices   PriceLookup
	unpriced UnpricedPolicy
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
	if refused := m.checkPriced(ctx, p, req.Completion.Model, false); refused != nil {
		return nil, refused
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
	if refused := m.checkPriced(ctx, p, req.Completion.Model, false); refused != nil {
		return nil, refused
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

	p, err := m.selectEmbeddingProvider(ctx, req.Embedding)
	if err != nil {
		return nil, err
	}
	if refused := m.checkPriced(ctx, p, req.Embedding.Model, true); refused != nil {
		return nil, refused
	}

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

// selectProvider picks the provider for a completion or a stream.
func (m *ProviderCallMiddleware) selectProvider(ctx context.Context, req *pipeline.Request) (provider.Provider, error) {
	if name := req.Completion.Provider; name != "" {
		return m.named(name)
	}
	if name := stateString(req, pipeline.StateAliasProvider); name != "" {
		return m.named(name)
	}

	allProviders := m.providers.All()
	if len(allProviders) == 0 {
		return nil, pipeline.Permanent(errors.New("nexus: no providers registered"))
	}
	if listed := m.listing(ctx, allProviders, req.Completion.Model); len(listed) > 0 {
		return m.route(ctx, req.Completion, listed)
	}
	return m.route(ctx, req.Completion, allProviders)
}

// selectEmbeddingProvider picks the provider for an embedding. A named
// provider that cannot embed is refused, never replaced by one that can.
func (m *ProviderCallMiddleware) selectEmbeddingProvider(ctx context.Context, emb *provider.EmbeddingRequest) (provider.Provider, error) {
	if name := emb.Provider; name != "" {
		p, err := m.named(name)
		if err != nil {
			return nil, err
		}
		if !p.Capabilities().Supports("embeddings") {
			return nil, pipeline.Permanent(&pipeline.RefusalError{
				Code:    pipeline.CodeInvalidRequest,
				Status:  400,
				Message: fmt.Sprintf("provider %s does not embed", name),
				Cause:   provider.ErrNotSupported,
			})
		}
		return p, nil
	}

	embedders := m.providers.WithCapability("embeddings")
	if len(embedders) == 0 {
		return nil, pipeline.Permanent(errors.New("nexus: no providers support embeddings"))
	}
	if listed := m.listing(ctx, embedders, emb.Model); len(listed) > 0 {
		// Strategies route completion requests; this one carries what an
		// embedding has in common with them.
		return m.route(ctx, &provider.CompletionRequest{
			Model:    emb.Model,
			TenantID: emb.TenantID,
			KeyID:    emb.KeyID,
		}, listed)
	}
	return embedders[0], nil
}

// named returns the registered provider called name, or refuses the request.
func (m *ProviderCallMiddleware) named(name string) (provider.Provider, error) {
	p, ok := m.providers.Get(name)
	if !ok {
		return nil, pipeline.Permanent(fmt.Errorf("%w: %q", ErrUnknownProvider, name))
	}
	return p, nil
}

// listing keeps the candidates whose catalog lists modelID, in order.
func (m *ProviderCallMiddleware) listing(ctx context.Context, candidates []provider.Provider, modelID string) []provider.Provider {
	if m.catalog == nil || modelID == "" {
		return nil
	}
	var listed []provider.Provider
	for _, p := range candidates {
		if m.catalog.Lists(ctx, p.Name(), modelID) {
			listed = append(listed, p)
		}
	}
	return listed
}

// route asks the router to choose among candidates, or takes the first
// when no router is configured.
func (m *ProviderCallMiddleware) route(ctx context.Context, req *provider.CompletionRequest, candidates []provider.Provider) (provider.Provider, error) {
	if m.router == nil {
		return candidates[0], nil
	}
	p, err := m.router.Route(ctx, req, candidates)
	if err != nil {
		return nil, pipeline.Permanent(fmt.Errorf("nexus: routing: %w", err))
	}
	return p, nil
}

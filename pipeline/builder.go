package pipeline

import (
	"context"
	"sort"

	"github.com/xraph/nexus/provider"
)

// Builder creates a pipeline from ordered middleware.
type Builder struct {
	middlewares []Middleware
}

// NewBuilder creates a new pipeline builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// Use adds middleware to the pipeline.
func (b *Builder) Use(m ...Middleware) *Builder {
	b.middlewares = append(b.middlewares, m...)
	return b
}

// Build creates a pipeline Service from the registered middleware,
// sorted by priority (lower = earlier).
func (b *Builder) Build() Service {
	sorted := make([]Middleware, len(b.middlewares))
	copy(sorted, b.middlewares)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority() < sorted[j].Priority()
	})
	return &pipelineImpl{middlewares: sorted}
}

// pipelineImpl executes middleware in priority order.
type pipelineImpl struct {
	middlewares []Middleware
}

func (p *pipelineImpl) Execute(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	pReq := &Request{
		Completion: req,
		Type:       RequestCompletion,
		State:      make(map[string]any),
	}
	if req.Stream {
		pReq.Type = RequestStream
	}

	resp, err := p.run(ctx, pReq, 0)
	if err != nil {
		return nil, err
	}
	return resp.Completion, nil
}

func (p *pipelineImpl) ExecuteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	pReq := &Request{
		Completion: req,
		Type:       RequestStream,
		State:      make(map[string]any),
	}

	resp, err := p.run(ctx, pReq, 0)
	if err != nil {
		return nil, err
	}
	return resp.Stream, nil
}

func (p *pipelineImpl) ExecuteEmbedding(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	pReq := &Request{
		Embedding: req,
		Type:      RequestEmbedding,
		State:     make(map[string]any),
	}

	resp, err := p.run(ctx, pReq, 0)
	if err != nil {
		return nil, err
	}
	return resp.Embedding, nil
}

// run recursively calls each middleware in priority order.
func (p *pipelineImpl) run(ctx context.Context, req *Request, idx int) (*Response, error) {
	if idx >= len(p.middlewares) {
		// End of chain — no more middleware
		return &Response{}, nil
	}

	mw := p.middlewares[idx]
	next := func(ctx context.Context) (*Response, error) {
		return p.run(ctx, req, idx+1)
	}

	return mw.Process(ctx, req, next)
}

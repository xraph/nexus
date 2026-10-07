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

// Build creates a pipeline Service. Non-terminal middleware runs in
// priority order (lower = earlier, ties keep the order they were added);
// the one Terminal runs last.
func (b *Builder) Build() (Service, error) {
	var rest []Middleware
	var term Middleware
	for _, m := range b.middlewares {
		if _, ok := m.(Terminal); ok {
			if term != nil {
				return nil, ErrManyTerminals
			}
			term = m
			continue
		}
		rest = append(rest, m)
	}
	if term == nil {
		return nil, ErrNoTerminal
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].Priority() < rest[j].Priority() })
	return &pipelineImpl{middlewares: append(rest, term)}, nil
}

// pipelineImpl executes middleware in priority order.
type pipelineImpl struct {
	middlewares []Middleware
}

// Stages lists the stages in the order they run.
func (p *pipelineImpl) Stages() []Stage {
	out := make([]Stage, len(p.middlewares))
	for i, m := range p.middlewares {
		_, term := m.(Terminal)
		out[i] = Stage{Name: m.Name(), Priority: m.Priority(), Terminal: term}
	}
	return out
}

func (p *pipelineImpl) Execute(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	// A request that asks for a stream cannot be answered with one response.
	// ExecuteStream is the way in for those.
	if req.Stream {
		return nil, &RefusalError{Code: CodeInvalidRequest, Status: 400, Message: "a stream request needs CompleteStream"}
	}
	pReq := &Request{
		Completion: req,
		Type:       RequestCompletion,
		State:      make(map[string]any),
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

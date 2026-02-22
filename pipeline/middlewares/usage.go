package middlewares

import (
	"context"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/usage"
)

// UsageMiddleware records usage for each request.
type UsageMiddleware struct {
	usage usage.Service
}

// NewUsage creates a usage tracking middleware.
func NewUsage(u usage.Service) *UsageMiddleware {
	return &UsageMiddleware{usage: u}
}

func (m *UsageMiddleware) Name() string  { return "usage" }
func (m *UsageMiddleware) Priority() int { return 550 } // After everything else

func (m *UsageMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.usage == nil {
		return next(ctx)
	}

	start := time.Now()
	resp, err := next(ctx)
	elapsed := time.Since(start)

	// Record usage even on error (to track failed requests)
	rec := &usage.Record{
		ID:        id.NewUsageID(),
		Provider:  pipeline.ProviderName(ctx),
		Latency:   elapsed,
		CreatedAt: time.Now(),
	}

	if req.Completion != nil {
		rec.Model = req.Completion.Model
	}

	if providerName, ok := req.State["provider_name"].(string); ok {
		rec.Provider = providerName
	}

	if err != nil {
		rec.StatusCode = 500
	} else {
		rec.StatusCode = 200
		if resp != nil && resp.Completion != nil {
			rec.PromptTokens = resp.Completion.Usage.PromptTokens
			rec.CompletionTokens = resp.Completion.Usage.CompletionTokens
			rec.TotalTokens = resp.Completion.Usage.TotalTokens
			rec.Cached = resp.Completion.Cached
			rec.CostUSD = resp.Completion.Cost
		}
	}

	// Record asynchronously (non-blocking)
	go func() {
		_ = m.usage.Record(context.Background(), rec)
	}()

	return resp, err
}

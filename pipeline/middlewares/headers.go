package middlewares

import (
	"context"
	"time"

	"github.com/xraph/nexus/pipeline"
)

// HeadersMiddleware adds standard X-Nexus-* response headers to the pipeline context.
// These are later read by the HTTP handler to set actual HTTP headers.
type HeadersMiddleware struct {
	gatewayID string
}

// NewHeaders creates a response headers middleware.
func NewHeaders(gatewayID string) *HeadersMiddleware {
	return &HeadersMiddleware{gatewayID: gatewayID}
}

func (m *HeadersMiddleware) Name() string  { return "headers" }
func (m *HeadersMiddleware) Priority() int { return 500 } // After everything else

func (m *HeadersMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	start := time.Now()
	resp, err := next(ctx)

	// Store header data in state for the HTTP layer to read
	req.State["x-nexus-request-id"] = pipeline.RequestID(ctx)
	req.State["x-nexus-provider"] = pipeline.ProviderName(ctx)
	req.State["x-nexus-cache-hit"] = pipeline.CacheHit(ctx)
	req.State["x-nexus-latency-ms"] = time.Since(start).Milliseconds()
	req.State["x-nexus-gateway"] = m.gatewayID

	if providerName, ok := req.State["provider_name"].(string); ok && providerName != "" {
		req.State["x-nexus-provider"] = providerName
	}

	return resp, err
}

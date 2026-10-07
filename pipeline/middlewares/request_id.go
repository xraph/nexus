package middlewares

import (
	"context"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/pipeline"
)

// RequestIDMiddleware gives every request a req_ id in context, unless the
// caller set one, so every later stage and the usage record can name it.
type RequestIDMiddleware struct{}

// NewRequestID creates the request id middleware.
func NewRequestID() *RequestIDMiddleware { return &RequestIDMiddleware{} }

func (*RequestIDMiddleware) Name() string  { return "request_id" }
func (*RequestIDMiddleware) Priority() int { return 5 }

func (*RequestIDMiddleware) Process(ctx context.Context, _ *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if pipeline.RequestID(ctx) == "" {
		ctx = pipeline.WithRequestID(ctx, id.NewRequestID().String())
	}
	return next(ctx)
}

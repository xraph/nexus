package middlewares

import (
	"context"
	"fmt"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/pipeline"
)

// ErrInvalidIdentity reports a tenant or key id that does not parse, or a
// request whose context and fields name different tenants or keys. It is a
// pipeline.Refusal (invalid_request, 400): the request is refused before any
// provider is called, and the usage stage records it unattributed at $0,
// because neither identity it named can be trusted. The identity stage wraps
// it with the detail, so match it with errors.Is.
var ErrInvalidIdentity error = &pipeline.RefusalError{
	Code:         pipeline.CodeInvalidRequest,
	Status:       400,
	Message:      "invalid tenant or key id",
	Unattributed: true,
}

// IdentityMiddleware makes the tenant and key a request is attributed to
// agree in both places later stages read them: the pipeline context (set by
// an authenticating HTTP edge) and the request's TenantID/KeyID fields (set
// by an in-process Go caller). Stages that wrap it, such as usage, read the
// context they were given and fall back to the request fields after the
// call returns, because a context set here is not visible to them. Both may
// be empty: an unattributed request is allowed.
type IdentityMiddleware struct{}

// NewIdentity creates the identity middleware.
func NewIdentity() *IdentityMiddleware { return &IdentityMiddleware{} }

func (*IdentityMiddleware) Name() string  { return "identity" }
func (*IdentityMiddleware) Priority() int { return 30 }

func (*IdentityMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	reqTenant, reqKey := requestIdentity(req)
	tenant, err := reconcile("tenant", pipeline.TenantID(ctx), reqTenant)
	if err != nil {
		return nil, err
	}
	key, err := reconcile("key", pipeline.KeyID(ctx), reqKey)
	if err != nil {
		return nil, err
	}
	if tenant != "" {
		if _, err := id.ParseTenantID(tenant); err != nil {
			return nil, fmt.Errorf("%w: tenant %q", ErrInvalidIdentity, tenant)
		}
	}
	if key != "" {
		if _, err := id.ParseKeyID(key); err != nil {
			return nil, fmt.Errorf("%w: key %q", ErrInvalidIdentity, key)
		}
	}
	setRequestIdentity(req, tenant, key)
	ctx = pipeline.WithTenantID(pipeline.WithKeyID(ctx, key), tenant)
	return next(ctx)
}

func reconcile(what, fromCtx, fromReq string) (string, error) {
	switch {
	case fromCtx == "":
		return fromReq, nil
	case fromReq == "" || fromReq == fromCtx:
		return fromCtx, nil
	}
	return "", fmt.Errorf("%w: %s is %q in context but %q on the request", ErrInvalidIdentity, what, fromCtx, fromReq)
}

func requestIdentity(req *pipeline.Request) (tenant, key string) {
	switch {
	case req.Completion != nil:
		return req.Completion.TenantID, req.Completion.KeyID
	case req.Embedding != nil:
		return req.Embedding.TenantID, req.Embedding.KeyID
	}
	return "", ""
}

func setRequestIdentity(req *pipeline.Request, tenant, key string) {
	switch {
	case req.Completion != nil:
		req.Completion.TenantID, req.Completion.KeyID = tenant, key
	case req.Embedding != nil:
		req.Embedding.TenantID, req.Embedding.KeyID = tenant, key
	}
}

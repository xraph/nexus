package middlewares

import (
	"context"
	"errors"
	"slices"

	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/tenant"
)

// TenantGetter and KeyGetter are the reads the access stage needs.
// tenant.Service and key.Service satisfy them.
type TenantGetter interface {
	Get(ctx context.Context, id string) (*tenant.Tenant, error)
}

// KeyGetter reads a key by id. key.Service.Get derives expiry, so an expired
// key reads as key.KeyExpired.
type KeyGetter interface {
	Get(ctx context.Context, id string) (*key.APIKey, error)
}

// AccessMiddleware refuses a request whose tenant is not active or whose key
// lacks the scope the request needs. It runs inside identity, so the tenant
// and key are in the context, and publishes the tenant for the quota stage.
// A request that names no tenant is not checked.
type AccessMiddleware struct {
	tenants TenantGetter
	keys    KeyGetter
}

// NewAccess returns the access stage.
func NewAccess(tenants TenantGetter, keys KeyGetter) *AccessMiddleware {
	return &AccessMiddleware{tenants: tenants, keys: keys}
}

func (*AccessMiddleware) Name() string  { return "access" }
func (*AccessMiddleware) Priority() int { return 40 }

type tenantCtxKey struct{}

// TenantFromContext returns the tenant the access stage loaded.
func TenantFromContext(ctx context.Context) (*tenant.Tenant, bool) {
	t, ok := ctx.Value(tenantCtxKey{}).(*tenant.Tenant)
	return t, ok && t != nil
}

func refuse(code string, status int, msg string) *pipeline.RefusalError {
	return &pipeline.RefusalError{Code: code, Status: status, Message: msg}
}

func (m *AccessMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	tenantID := pipeline.TenantID(ctx)
	if tenantID == "" {
		return next(ctx)
	}
	t, err := m.tenants.Get(ctx, tenantID)
	switch {
	case errors.Is(err, tenant.ErrNotFound):
		r := refuse(pipeline.CodeForbidden, 403, "unknown tenant")
		r.Unattributed = true
		return nil, r
	case err != nil:
		r := refuse(pipeline.CodeUnavailable, 503, "tenant lookup failed")
		r.Cause = err
		return nil, r
	case t.Status != tenant.StatusActive:
		return nil, refuse(pipeline.CodeForbidden, 403, "tenant is "+string(t.Status))
	}
	if keyID := pipeline.KeyID(ctx); keyID != "" {
		scopes, ok := pipeline.Scopes(ctx)
		if !ok {
			k, err := m.keys.Get(ctx, keyID)
			switch {
			case errors.Is(err, key.ErrNotFound):
				return nil, refuse(pipeline.CodeUnauthenticated, 401, "unknown api key")
			case err != nil:
				r := refuse(pipeline.CodeUnavailable, 503, "key lookup failed")
				r.Cause = err
				return nil, r
			case k.TenantID.String() != tenantID:
				return nil, refuse(pipeline.CodeForbidden, 403, "the key belongs to another tenant")
			case k.Status != key.KeyActive:
				return nil, refuse(pipeline.CodeUnauthenticated, 401, "api key "+string(k.Status))
			}
			scopes = k.Scopes
		}
		need := "completions"
		if req.Type == pipeline.RequestEmbedding {
			need = "embeddings"
		}
		if !slices.Contains(scopes, need) {
			return nil, refuse(pipeline.CodeForbidden, 403, "the key lacks the "+need+" scope")
		}
	}
	return next(context.WithValue(ctx, tenantCtxKey{}, t))
}

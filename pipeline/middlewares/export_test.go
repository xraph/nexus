package middlewares

import (
	"context"

	"github.com/xraph/nexus/tenant"
)

// WithTenantForTest publishes t the way the access stage does, so a stage
// that reads the tenant can be tested on its own.
func WithTenantForTest(ctx context.Context, t *tenant.Tenant) context.Context {
	return context.WithValue(ctx, tenantCtxKey{}, t)
}

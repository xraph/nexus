package tenant

import (
	"context"

	"github.com/xraph/nexus/id"
)

// Events receives tenant lifecycle events. *plugin.Registry satisfies it.
type Events interface {
	EmitTenantCreated(ctx context.Context, tenantID id.TenantID)
	EmitTenantDisabled(ctx context.Context, tenantID id.TenantID)
}

// Option configures the tenant service.
type Option func(*service)

// WithEvents reports created tenants, and tenants that leave active.
func WithEvents(e Events) Option { return func(s *service) { s.events = e } }

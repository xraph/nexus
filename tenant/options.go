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

// InUseFunc reports whether a tenant still has keys that belong to it. The
// gateway builds it from the key store. It is a func, not the key store,
// because the key package imports this one.
type InUseFunc func(ctx context.Context, tenantID string) (bool, error)

// Option configures the tenant service.
type Option func(*service)

// WithEvents reports created tenants, and tenants that leave active.
func WithEvents(e Events) Option { return func(s *service) { s.events = e } }

// WithInUse makes Delete refuse, with ErrInUse, a tenant the check reports as
// in use. A store may refuse too: Postgres rejects a delete that would orphan
// a key or a usage row, and says so with the same error. Without a check the
// service leaves keys to the store.
func WithInUse(f InUseFunc) Option { return func(s *service) { s.inUse = f } }

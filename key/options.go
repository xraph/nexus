package key

import (
	"context"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/tenant"
)

// TenantFinder lets Create check that a key's tenant exists. tenant.Store
// satisfies it.
type TenantFinder interface {
	FindByID(ctx context.Context, id string) (*tenant.Tenant, error)
}

// Events receives key lifecycle events. *plugin.Registry satisfies it.
type Events interface {
	EmitKeyCreated(ctx context.Context, keyID id.KeyID, tenantID id.TenantID)
	EmitKeyRevoked(ctx context.Context, keyID id.KeyID)
}

// Option configures the key service.
type Option func(*service)

// WithTenants makes Create refuse a key for a tenant that does not exist.
func WithTenants(t TenantFinder) Option { return func(s *service) { s.tenants = t } }

// WithEvents reports created and revoked keys.
func WithEvents(e Events) Option { return func(s *service) { s.events = e } }

// WithClock replaces time.Now, for tests.
func WithClock(now func() time.Time) Option { return func(s *service) { s.now = now } }

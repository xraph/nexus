// Package store defines the aggregate persistence interface for Nexus.
package store

import (
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// Store is the aggregate persistence interface.
// Each subsystem's Store interface is composed here.
// Note: Audit data is NOT stored here — it flows through the audit_hook
// extension to Chronicle, which owns audit persistence.
type Store interface {
	Tenants() tenant.Store
	Keys() key.Store
	Usage() usage.Store

	// Lifecycle
	Migrate() error
	Close() error
}

package extension

import (
	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/store"
)

// Option configures the Nexus Forge extension.
type Option func(*Extension)

// WithProvider adds an LLM provider to the gateway.
func WithProvider(p provider.Provider) Option {
	return func(e *Extension) {
		e.gatewayOpts = append(e.gatewayOpts, nexus.WithProvider(p))
	}
}

// WithDatabase sets the persistence store.
func WithDatabase(s store.Store) Option {
	return func(e *Extension) {
		e.gatewayOpts = append(e.gatewayOpts, nexus.WithDatabase(s))
	}
}

// WithGatewayOption adds a raw nexus.Option directly to the gateway.
func WithGatewayOption(opt nexus.Option) Option {
	return func(e *Extension) {
		e.gatewayOpts = append(e.gatewayOpts, opt)
	}
}

// WithBasePath sets the HTTP base path for gateway routes.
func WithBasePath(path string) Option {
	return func(e *Extension) {
		e.config.BasePath = path
	}
}

// WithConfig sets the Forge extension configuration.
func WithConfig(cfg Config) Option {
	return func(e *Extension) { e.config = cfg }
}

// WithDisableRoutes prevents HTTP route registration.
func WithDisableRoutes() Option {
	return func(e *Extension) { e.config.DisableRoutes = true }
}

// WithDisableMigrate prevents auto-migration on start.
func WithDisableMigrate() Option {
	return func(e *Extension) { e.config.DisableMigrate = true }
}

// WithRequireConfig requires config to be present in YAML files.
// If true and no config is found, Register returns an error.
func WithRequireConfig(require bool) Option {
	return func(e *Extension) { e.config.RequireConfig = require }
}

// WithGroveDatabase sets the name of the grove.DB to resolve from the DI container.
// The extension will auto-construct the appropriate store backend (postgres/sqlite/mongo)
// based on the grove driver type. Pass an empty string to use the default (unnamed) grove.DB.
func WithGroveDatabase(name string) Option {
	return func(e *Extension) {
		e.config.GroveDatabase = name
		e.useGrove = true
	}
}

package extension

import (
	"log/slog"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/store"
)

// Option configures the Nexus Forge extension.
type Option func(*Extension)

// WithProvider adds an LLM provider to the gateway.
func WithProvider(p provider.Provider) Option {
	return func(e *Extension) {
		e.opts = append(e.opts, nexus.WithProvider(p))
	}
}

// WithDatabase sets the persistence store.
func WithDatabase(s store.Store) Option {
	return func(e *Extension) {
		e.opts = append(e.opts, nexus.WithDatabase(s))
	}
}

// WithGatewayOption adds a raw nexus.Option.
func WithGatewayOption(opt nexus.Option) Option {
	return func(e *Extension) {
		e.opts = append(e.opts, opt)
	}
}

// WithLogger sets a custom logger for the extension.
func WithLogger(l *slog.Logger) Option {
	return func(e *Extension) {
		e.logger = l
	}
}

// WithBasePath sets the HTTP base path for gateway routes.
func WithBasePath(path string) Option {
	return func(e *Extension) {
		e.opts = append(e.opts, nexus.WithBasePath(path))
	}
}

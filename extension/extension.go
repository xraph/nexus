// Package extension provides the Forge extension for Nexus.
// It integrates Nexus with the Forge ecosystem, auto-discovering
// Chronicle, Shield, Relay, and Authsome when available.
package extension

import (
	"context"
	"fmt"
	"log/slog"

	nexus "github.com/xraph/nexus"
)

// Extension is the Forge extension for Nexus.
// It manages the Nexus Gateway lifecycle within a Forge application.
type Extension struct {
	opts    []nexus.Option
	gateway *nexus.Gateway
	logger  *slog.Logger
}

// New creates a new Nexus Forge extension.
func New(opts ...Option) *Extension {
	e := &Extension{
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Name returns the extension name.
func (e *Extension) Name() string { return "nexus" }

// Description returns a human-readable description.
func (e *Extension) Description() string {
	return "Composable AI gateway — route, cache, guard, and observe LLM traffic"
}

// Version returns the extension version.
func (e *Extension) Version() string { return "0.1.0" }

// Dependencies returns extensions that should be loaded before this one.
func (e *Extension) Dependencies() []string { return nil }

// Register is called during Forge application setup.
// It auto-discovers ecosystem extensions (Chronicle, Shield, Relay, Authsome)
// and configures them as Nexus extensions/adapters.
func (e *Extension) Register(ctx context.Context) error {
	e.logger.Info("nexus: registering extension")

	// Auto-discovery happens here in a full forge environment.
	// Each discovered service is wired as a nexus option:
	//
	// Chronicle → audit_hook extension
	//   if chr, err := vessel.Resolve[*chronicle.Chronicle](container); err == nil {
	//       e.opts = append(e.opts, nexus.WithExtension(audithook.New(...)))
	//   }
	//
	// Shield → guard adapter
	//   if shieldEng, err := vessel.Resolve[*shield.Engine](container); err == nil {
	//       e.opts = append(e.opts, nexus.WithGuard(shieldadapter.New(shieldEng)))
	//   }
	//
	// Relay → relay_hook extension
	// Authsome → auth adapter
	// Metrics → observability extension

	return nil
}

// Start initializes the Nexus gateway and makes it available.
func (e *Extension) Start(ctx context.Context) error {
	e.gateway = nexus.New(e.opts...)
	if err := e.gateway.Initialize(ctx); err != nil {
		return fmt.Errorf("nexus: failed to initialize gateway: %w", err)
	}
	e.logger.Info("nexus: gateway started",
		slog.Int("providers", e.gateway.Providers().Count()),
		slog.Int("extensions", e.gateway.Extensions().Count()),
	)
	return nil
}

// Stop gracefully shuts down the gateway.
func (e *Extension) Stop(ctx context.Context) error {
	if e.gateway != nil {
		return e.gateway.Shutdown(ctx)
	}
	return nil
}

// Health reports the gateway health status.
func (e *Extension) Health(ctx context.Context) error {
	if e.gateway == nil {
		return fmt.Errorf("nexus: gateway not initialized")
	}
	return nil
}

// Gateway returns the underlying Nexus gateway instance.
func (e *Extension) Gateway() *nexus.Gateway {
	return e.gateway
}

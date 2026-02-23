// Package nexus is a composable AI gateway library for Go.
// Route, cache, guard, and observe LLM traffic at scale.
//
// Nexus is a library, not a SaaS. Import it, compose your AI gateway,
// and own your infrastructure.
//
//	gw := nexus.New(
//	    nexus.WithProvider(openai.New("sk-...")),
//	    nexus.WithProvider(anthropic.New("sk-ant-...")),
//	    nexus.WithRouter(router.NewCostOptimized()),
//	    nexus.WithCache(cache.NewRedis(redisClient)),
//	    nexus.WithGuard(guard.NewPII(guard.ActionRedact)),
//	)
//	gw.Initialize(ctx)
//	gw.Mount(router, "/ai")
package nexus

import (
	"context"
	"net/http"
	"time"

	"github.com/xraph/nexus/auth"
	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/observability"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/plugin"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
	"github.com/xraph/nexus/router/strategies"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/transform"
	"github.com/xraph/nexus/usage"
)

// Gateway is the root Nexus instance.
// It can be used standalone or mounted into a Forge application.
type Gateway struct {
	config *Config
	engine *Engine
	store  store.Store
	auth   auth.Provider
	logger Logger

	// Core services (all interface-based)
	providers provider.Registry
	router    router.Service
	pipeline  pipeline.Service
	cache     cache.Service
	guard     guard.Service
	tenant    tenant.Service
	key       key.Service
	usage     usage.Service
	model     model.Service

	// Model alias registry
	aliasRegistry model.AliasRegistry

	// Extension registry — audit_hook, observability, relay_hook, etc.
	extensions *plugin.Registry

	// Observability
	tracer      observability.Tracer
	transforms  *transform.Registry
	healthTrack provider.HealthTracker

	// Custom middleware to add to the pipeline
	customMiddleware []pipeline.Middleware

	initialized bool
}

// New creates a new Nexus Gateway with the given options.
func New(opts ...Option) *Gateway {
	gw := &Gateway{
		config:     DefaultConfig(),
		providers:  provider.NewRegistry(),
		extensions: plugin.NewRegistry(),
	}

	for _, opt := range opts {
		opt(gw)
	}

	return gw
}

// Initialize sets up all services and validates configuration.
func (gw *Gateway) Initialize(ctx context.Context) error {
	if gw.initialized {
		return nil
	}

	// Set defaults for unset services
	if gw.auth == nil {
		gw.auth = auth.NewNoop()
	}
	if gw.store == nil {
		gw.store = store.NewMemory()
	}
	if gw.logger == nil {
		gw.logger = NewDefaultLogger(gw.config.LogLevel)
	}

	// Initialize model service
	if gw.model == nil {
		gw.model = model.NewService(gw.aliasRegistry, gw.providers)
	}

	// Default router: priority strategy (registration order)
	if gw.router == nil {
		gw.router = router.NewService(strategies.NewPriority())
	}

	// Initialize engine
	gw.engine = newEngine(gw)

	// Build default pipeline if not set
	if gw.pipeline == nil {
		gw.pipeline = gw.buildDefaultPipeline()
	}

	gw.initialized = true
	gw.logger.Info("nexus gateway initialized",
		"providers", gw.providers.Count(),
		"extensions", gw.extensions.Count(),
		"base_path", gw.config.BasePath,
	)
	return nil
}

// buildDefaultPipeline creates the standard middleware chain.
// Middleware is sorted by priority (lower = earlier), so the order
// of b.Use() calls here doesn't matter — priority determines execution order.
func (gw *Gateway) buildDefaultPipeline() pipeline.Service {
	b := pipeline.NewBuilder()

	// Priority 10: Tracing (if configured)
	if gw.tracer != nil {
		b.Use(observability.NewTracingMiddleware(gw.tracer))
	}

	// Priority 20: Timeout (if configured)
	if gw.config.DefaultTimeout > 0 {
		b.Use(middlewares.NewTimeout(gw.config.DefaultTimeout))
	}

	// Priority 150: Input guardrails (if configured)
	if gw.guard != nil {
		b.Use(middlewares.NewGuardrail(gw.guard))
	}

	// Priority 200: Transforms (if configured)
	if gw.transforms != nil {
		b.Use(middlewares.NewTransform(gw.transforms))
	}

	// Priority 250: Alias resolution (if configured)
	if gw.aliasRegistry != nil {
		b.Use(middlewares.NewAlias(gw.aliasRegistry))
	}

	// Priority 280: Cache (if configured)
	if gw.cache != nil {
		b.Use(middlewares.NewCache(gw.cache))
	}

	// Priority 340: Retry (if resilience configured)
	if gw.config.DefaultMaxRetries > 0 {
		b.Use(middlewares.NewRetry(gw.config.DefaultMaxRetries, 500*time.Millisecond, 2.0))
	}

	// Priority 350: Core provider call (always present)
	b.Use(middlewares.NewProviderCall(gw.router, gw.providers))

	// Priority 500: Response headers
	b.Use(middlewares.NewHeaders("nexus"))

	// Priority 550: Usage tracking (if store available)
	if gw.usage != nil {
		b.Use(middlewares.NewUsage(gw.usage))
	}

	// Custom middleware (user-provided, any priority)
	for _, m := range gw.customMiddleware {
		b.Use(m)
	}

	return b.Build()
}

// Mount registers Nexus HTTP handlers on the given router.
func (gw *Gateway) Mount(mux Router, basePath ...string) {
	path := gw.config.BasePath
	if len(basePath) > 0 {
		path = basePath[0]
	}
	mountHandlers(gw, mux, path)
}

// Engine returns the core engine for programmatic usage.
func (gw *Gateway) Engine() *Engine { return gw.engine }

// Config returns the gateway configuration.
func (gw *Gateway) Config() *Config { return gw.config }

// Store returns the persistence store.
func (gw *Gateway) Store() store.Store { return gw.store }

// Service Accessors

// Providers returns the provider registry.
func (gw *Gateway) Providers() provider.Registry { return gw.providers }

// RouterService returns the routing service.
func (gw *Gateway) RouterService() router.Service { return gw.router }

// Cache returns the cache service.
func (gw *Gateway) Cache() cache.Service { return gw.cache }

// Guard returns the guard service.
func (gw *Gateway) Guard() guard.Service { return gw.guard }

// Tenants returns the tenant service.
func (gw *Gateway) Tenants() tenant.Service { return gw.tenant }

// Keys returns the API key service.
func (gw *Gateway) Keys() key.Service { return gw.key }

// Usage returns the usage service.
func (gw *Gateway) Usage() usage.Service { return gw.usage }

// Models returns the model service.
func (gw *Gateway) Models() model.Service { return gw.model }

// Extensions returns the extension registry.
func (gw *Gateway) Extensions() *plugin.Registry { return gw.extensions }

// Pipeline returns the pipeline service.
func (gw *Gateway) Pipeline() pipeline.Service { return gw.pipeline }

// Logger returns the gateway logger.
func (gw *Gateway) Logger() Logger { return gw.logger }

// Shutdown gracefully stops all services.
func (gw *Gateway) Shutdown(ctx context.Context) error {
	gw.logger.Info("nexus gateway shutting down")
	if gw.store != nil {
		return gw.store.Close()
	}
	return nil
}

// Router is a minimal interface for HTTP routing.
// Compatible with chi.Router, forge.Router, http.ServeMux, etc.
type Router interface {
	http.Handler
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler http.HandlerFunc)
}

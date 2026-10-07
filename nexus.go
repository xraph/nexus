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
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/nexus/auth"
	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/observability"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/plugin"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/ratelimit"
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

	// routerStrategy is the name of the routing strategy in use.
	routerStrategy string

	// priceBook prices requests for the usage stage.
	priceBook *model.PriceBook

	// usageMW is the usage stage, kept so Shutdown can flush it.
	usageMW *middlewares.UsageMiddleware

	// limiter counts requests and tokens per minute for the quota stage.
	limiter ratelimit.Limiter

	// quotaMW is the quota stage, kept so the gateway can report limiter failures.
	quotaMW *middlewares.QuotaMiddleware

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

	// Stream lifecycle config — tunes per-chunk hook fan-out for streaming.
	streamLifecycleCfg middlewares.StreamLifecycleConfig

	// Stream cache (optional) for record-and-replay of streamed responses.
	streamCache    cache.StreamCache
	streamCacheCfg cache.StreamCacheOptions

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
func (gw *Gateway) Initialize(_ context.Context) error {
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
		gw.logger = NewNoopLogger()
	}

	if gw.tenant == nil {
		var te tenant.Events
		if gw.extensions != nil {
			te = gw.extensions
		}
		gw.tenant = tenant.NewService(gw.store.Tenants(), tenant.WithEvents(te))
	}
	if gw.key == nil {
		var ke key.Events
		if gw.extensions != nil {
			ke = gw.extensions
		}
		gw.key = key.NewService(gw.store.Keys(), key.WithTenants(gw.store.Tenants()), key.WithEvents(ke))
	}
	if gw.usage == nil {
		gw.usage = usage.NewService(gw.store.Usage())
	}
	if gw.config.EnableCache && gw.cache == nil && gw.streamCache == nil {
		gw.cache = cache.NewService(stores.NewMemory())
	}
	gw.priceBook = model.NewPriceBook(gw.providers)

	// Initialize model service
	if gw.model == nil {
		gw.model = model.NewService(gw.aliasRegistry, gw.providers)
	}

	// Default router: priority strategy (registration order)
	if gw.router == nil {
		s := strategies.NewPriority()
		gw.router = router.NewService(s)
		gw.routerStrategy = s.Name()
	}

	// Initialize engine
	gw.engine = newEngine(gw)

	if gw.limiter == nil {
		gw.limiter = ratelimit.NewMemory()
	}

	// Build default pipeline if not set
	if gw.pipeline == nil {
		p, err := gw.buildDefaultPipeline()
		if err != nil {
			return err
		}
		gw.pipeline = p
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
// of b.Use() calls here doesn't matter: priority determines execution order.
func (gw *Gateway) buildDefaultPipeline() (pipeline.Service, error) {
	b := pipeline.NewBuilder()

	// Priority 5: request id
	b.Use(middlewares.NewRequestID())

	// Priority 10: Tracing (if configured)
	if gw.tracer != nil {
		b.Use(observability.NewTracingMiddleware(gw.tracer))
	}

	// Priority 15: Usage recording (unless turned off)
	if gw.config.EnableUsage && gw.usage != nil {
		gw.usageMW = middlewares.NewUsage(gw.usage, gw.priceBook, gw.logger)
		b.Use(gw.usageMW)
	}

	// Priority 20: Timeout (if configured)
	if gw.config.DefaultTimeout > 0 {
		b.Use(middlewares.NewTimeout(gw.config.DefaultTimeout))
	}

	// Priority 30: Identity (tenant isolation for the cache depends on it)
	b.Use(middlewares.NewIdentity())

	// Priority 40: tenant status and key scopes
	b.Use(middlewares.NewAccess(gw.tenant, gw.key))

	// Priority 50: token cap, daily requests, budget, RPM and TPM. The stage
	// reads usage from the store, so it needs the usage service.
	if gw.usage != nil {
		var budgetEvents middlewares.BudgetEvents
		if gw.extensions != nil {
			budgetEvents = gw.extensions
		}
		gw.quotaMW = middlewares.NewQuota(middlewares.QuotaConfig{
			Usage: gw.usage, Limiter: gw.limiter, Events: budgetEvents,
			GlobalRPM: gw.config.GlobalRateLimit, Log: gw.logger,
		})
		b.Use(gw.quotaMW)
	}

	// Priority 60: Stream lifecycle hooks (only meaningful when extensions
	// are registered; the middleware short-circuits when the registry is
	// empty or the request isn't a stream).
	if gw.extensions != nil {
		cfg := gw.streamLifecycleCfg
		if cfg.QuotaResolver == nil {
			cfg.QuotaResolver = middlewares.TenantStreamQuota
		}
		b.Use(middlewares.NewStreamLifecycle(gw.extensions, cfg))
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
	if gw.cache != nil || gw.streamCache != nil {
		mw := middlewares.NewCache(gw.cache)
		if gw.streamCache != nil {
			mw = mw.WithStreamCache(gw.streamCache, gw.streamCacheCfg)
		}
		b.Use(mw)
	}

	// Priority 340: Retry (if resilience configured)
	if gw.config.DefaultMaxRetries > 0 {
		b.Use(middlewares.NewRetry(gw.config.DefaultMaxRetries, 500*time.Millisecond, 2.0))
	}

	// Custom middleware (user-provided, any priority)
	for _, m := range gw.customMiddleware {
		b.Use(m)
	}

	// Priority 350: Core provider call (always present, always last)
	b.Use(middlewares.NewProviderCall(gw.router, gw.providers))

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

// RoutingStrategy is the name of the routing strategy in use.
func (gw *Gateway) RoutingStrategy() string { return gw.routerStrategy }

// Aliases lists the configured model aliases, or nil when there are none.
func (gw *Gateway) Aliases() []model.Alias {
	if gw.aliasRegistry == nil {
		return nil
	}
	return gw.aliasRegistry.List()
}

// Transforms returns the transform registry, or nil when none is configured.
func (gw *Gateway) Transforms() *transform.Registry { return gw.transforms }

// PipelineStages lists the pipeline's stages in the order they run, or nil
// for a custom pipeline that cannot list them.
func (gw *Gateway) PipelineStages() []pipeline.Stage {
	if in, ok := gw.pipeline.(pipeline.Inspector); ok {
		return in.Stages()
	}
	return nil
}

// UsageInsertErrors is how many usage records failed to store since start.
func (gw *Gateway) UsageInsertErrors() int64 {
	if gw.usageMW == nil {
		return 0
	}
	return gw.usageMW.InsertErrors()
}

// PriceBook returns the price book the usage stage prices requests with.
func (gw *Gateway) PriceBook() *model.PriceBook { return gw.priceBook }

// Limiter is the RPM and TPM limiter in use.
func (gw *Gateway) Limiter() ratelimit.Limiter { return gw.limiter }

// LimiterErrors counts limiter failures that let a request through.
func (gw *Gateway) LimiterErrors() int64 {
	if gw.quotaMW == nil {
		return 0
	}
	return gw.quotaMW.LimiterErrors()
}

// FlushUsage waits until every usage record taken so far is stored, without
// closing the stage. Shutdown flushes on its own.
func (gw *Gateway) FlushUsage(ctx context.Context) error {
	if gw.usageMW == nil {
		return nil
	}
	return gw.usageMW.Flush(ctx)
}

// Health checks the health of the Gateway.
func (gw *Gateway) Health(_ context.Context) error {
	return nil
}

// Shutdown gracefully stops all services. It stops the usage stage taking
// new records, waits until ctx is done for the streams still open and the
// records still being stored, then closes the store. When ctx ends first,
// the records not yet stored are lost: Shutdown logs how many and returns
// the flush error, joined with any error from closing the store.
func (gw *Gateway) Shutdown(ctx context.Context) error {
	gw.logger.Info("nexus gateway shutting down")
	var flushErr error
	if gw.usageMW != nil {
		gw.usageMW.Close()
		if err := gw.usageMW.Flush(ctx); err != nil {
			gw.logger.Warn("nexus: usage records not stored at shutdown", "pending", gw.usageMW.Pending(), "error", err)
			flushErr = fmt.Errorf("nexus: flushing usage records: %w", err)
		}
	}
	var closeErr error
	if gw.store != nil {
		closeErr = gw.store.Close()
	}
	return errors.Join(flushErr, closeErr)
}

// Router is a minimal interface for HTTP routing.
// Compatible with chi.Router, forge.Router, http.ServeMux, etc.
type Router interface {
	http.Handler
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler http.HandlerFunc)
}

package nexus

import (
	"time"

	"github.com/xraph/nexus/auth"
	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/observability"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/plugin"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/transform"
)

// Option configures a Gateway.
type Option func(*Gateway)

// WithConfig sets the gateway configuration.
func WithConfig(cfg *Config) Option {
	return func(gw *Gateway) { gw.config = cfg }
}

// WithDatabase sets the persistence store.
func WithDatabase(s store.Store) Option {
	return func(gw *Gateway) { gw.store = s }
}

// WithAuth sets the auth provider.
func WithAuth(a auth.Provider) Option {
	return func(gw *Gateway) { gw.auth = a }
}

// WithProvider registers an LLM provider.
func WithProvider(p provider.Provider) Option {
	return func(gw *Gateway) { gw.providers.Register(p) }
}

// WithRouter sets the routing strategy.
func WithRouter(r router.Strategy) Option {
	return func(gw *Gateway) {
		gw.router = router.NewService(r)
	}
}

// WithCache enables caching with the given cache store.
func WithCache(c cache.Cache) Option {
	return func(gw *Gateway) {
		gw.cache = cache.NewService(c)
		gw.config.EnableCache = true
	}
}

// WithGuard adds a guardrail to the pipeline.
func WithGuard(g guard.Guard) Option {
	return func(gw *Gateway) {
		if gw.guard == nil {
			gw.guard = guard.NewService()
		}
		gw.guard.Register(g)
	}
}

// WithMiddleware adds a custom middleware to the pipeline.
func WithMiddleware(m pipeline.Middleware) Option {
	return func(gw *Gateway) {
		gw.customMiddleware = append(gw.customMiddleware, m)
	}
}

// WithPipeline sets a fully custom pipeline, replacing the default.
func WithPipeline(p pipeline.Service) Option {
	return func(gw *Gateway) { gw.pipeline = p }
}

// WithLogger sets a custom logger.
func WithLogger(l Logger) Option {
	return func(gw *Gateway) { gw.logger = l }
}

// WithBasePath sets the HTTP base path.
func WithBasePath(path string) Option {
	return func(gw *Gateway) { gw.config.BasePath = path }
}

// WithRateLimit sets global rate limit (requests per minute).
func WithRateLimit(rpm int) Option {
	return func(gw *Gateway) { gw.config.GlobalRateLimit = rpm }
}

// WithExtension registers a lifecycle extension (audit_hook, observability, relay_hook, etc.).
func WithExtension(e plugin.Extension) Option {
	return func(gw *Gateway) { gw.extensions.Register(e) }
}

// WithAlias registers a model alias that maps a virtual model name
// to one or more concrete provider+model targets.
//
// Example:
//
//	nexus.WithAlias("fast", model.AliasTarget{Provider: "anthropic", Model: "claude-3.5-haiku"})
//	nexus.WithAlias("cheap",
//	    model.AliasTarget{Provider: "openai", Model: "gpt-4o-mini", Weight: 0.7},
//	    model.AliasTarget{Provider: "anthropic", Model: "claude-3.5-haiku", Weight: 0.3},
//	)
func WithAlias(name string, targets ...model.AliasTarget) Option {
	return func(gw *Gateway) {
		if gw.aliasRegistry == nil {
			gw.aliasRegistry = model.NewAliasRegistry()
		}
		if err := gw.aliasRegistry.Register(&model.Alias{
			Name:    name,
			Targets: targets,
		}); err != nil {
			return
		}
	}
}

// WithTracer sets a request tracer for observability.
func WithTracer(t observability.Tracer) Option {
	return func(gw *Gateway) { gw.tracer = t }
}

// WithTransforms sets the transform registry for input/output transforms.
func WithTransforms(r *transform.Registry) Option {
	return func(gw *Gateway) { gw.transforms = r }
}

// WithHealthTracker sets the provider health tracker.
func WithHealthTracker(h provider.HealthTracker) Option {
	return func(gw *Gateway) { gw.healthTrack = h }
}

// WithTimeout sets the default request timeout.
func WithTimeout(d time.Duration) Option {
	return func(gw *Gateway) { gw.config.DefaultTimeout = d }
}

// WithMaxRetries sets the default max retries.
func WithMaxRetries(n int) Option {
	return func(gw *Gateway) { gw.config.DefaultMaxRetries = n }
}

// WithTenantAlias registers a per-tenant model alias override.
func WithTenantAlias(tenantID, name string, targets ...model.AliasTarget) Option {
	return func(gw *Gateway) {
		if gw.aliasRegistry == nil {
			gw.aliasRegistry = model.NewAliasRegistry()
		}
		// Get or create the alias
		aliases := gw.aliasRegistry.List()
		var found bool
		for _, a := range aliases {
			if a.Name != name {
				continue
			}
			if a.TenantOverrides == nil {
				a.TenantOverrides = make(map[string][]model.AliasTarget)
			}
			a.TenantOverrides[tenantID] = targets
			if err := gw.aliasRegistry.Register(&a); err != nil {
				return
			}
			found = true
			break
		}
		if !found {
			if err := gw.aliasRegistry.Register(&model.Alias{
				Name:    name,
				Targets: targets,
				TenantOverrides: map[string][]model.AliasTarget{
					tenantID: targets,
				},
			}); err != nil {
				return
			}
		}
	}
}

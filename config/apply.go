package config

import (
	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/guard/guards"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/providers/anthropic"
	"github.com/xraph/nexus/providers/openai"
	"github.com/xraph/nexus/providers/opencompat"
	"github.com/xraph/nexus/router/strategies"
)

// Apply converts a GatewayConfig into Gateway options.
func Apply(cfg *GatewayConfig) []nexus.Option {
	var opts []nexus.Option

	// Server settings
	if cfg.Server.BasePath != "" {
		opts = append(opts, nexus.WithBasePath(cfg.Server.BasePath))
	}

	// Providers
	for _, pc := range cfg.Providers {
		switch pc.Type {
		case "openai":
			opts = append(opts, nexus.WithProvider(openai.New(pc.APIKey)))
		case "anthropic":
			opts = append(opts, nexus.WithProvider(anthropic.New(pc.APIKey)))
		case "opencompat":
			opts = append(opts, nexus.WithProvider(opencompat.New(pc.Name, pc.BaseURL, pc.APIKey)))
		}
	}

	// Routing strategy
	switch cfg.Routing.Strategy {
	case "priority":
		opts = append(opts, nexus.WithRouter(strategies.NewPriority(cfg.Routing.Priority...)))
	case "cost":
		opts = append(opts, nexus.WithRouter(strategies.NewCostOptimized()))
	case "latency":
		opts = append(opts, nexus.WithRouter(strategies.NewLatencyOptimized()))
	case "round_robin":
		opts = append(opts, nexus.WithRouter(strategies.NewRoundRobin()))
	case "weighted":
		opts = append(opts, nexus.WithRouter(strategies.NewWeighted(cfg.Routing.Weights)))
	}

	// Cache
	if cfg.Cache.Enabled {
		var cacheOpts []stores.MemoryOption
		if cfg.Cache.TTL > 0 {
			cacheOpts = append(cacheOpts, stores.WithTTL(cfg.Cache.TTL))
		}
		if cfg.Cache.MaxSize > 0 {
			cacheOpts = append(cacheOpts, stores.WithMaxSize(cfg.Cache.MaxSize))
		}
		opts = append(opts, nexus.WithCache(stores.NewMemory(cacheOpts...)))
	}

	// Guardrails
	if cfg.Guardrails.PII.Enabled {
		action := guard.ActionRedact
		switch cfg.Guardrails.PII.Action {
		case "block":
			action = guard.ActionBlock
		case "warn":
			action = guard.ActionWarn
		}
		opts = append(opts, nexus.WithGuard(guards.NewPII(action)))
	}
	if cfg.Guardrails.Injection {
		opts = append(opts, nexus.WithGuard(guards.NewInjection()))
	}
	if len(cfg.Guardrails.Blocklist) > 0 {
		opts = append(opts, nexus.WithGuard(guards.NewContentFilter(guard.ActionBlock, cfg.Guardrails.Blocklist...)))
	}

	// Aliases
	for _, alias := range cfg.Aliases {
		targets := make([]model.AliasTarget, len(alias.Targets))
		for i, t := range alias.Targets {
			targets[i] = model.AliasTarget{
				Provider: t.Provider,
				Model:    t.Model,
				Weight:   t.Weight,
			}
		}
		opts = append(opts, nexus.WithAlias(alias.Name, targets...))
	}

	return opts
}

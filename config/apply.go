package config

import (
	"os"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/guard/guards"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/ai21"
	"github.com/xraph/nexus/providers/anthropic"
	"github.com/xraph/nexus/providers/anyscale"
	"github.com/xraph/nexus/providers/azureopenai"
	"github.com/xraph/nexus/providers/bedrock"
	"github.com/xraph/nexus/providers/cerebras"
	"github.com/xraph/nexus/providers/cohere"
	"github.com/xraph/nexus/providers/deepinfra"
	"github.com/xraph/nexus/providers/deepseek"
	"github.com/xraph/nexus/providers/fireworks"
	"github.com/xraph/nexus/providers/gemini"
	"github.com/xraph/nexus/providers/groq"
	"github.com/xraph/nexus/providers/hyperbolic"
	"github.com/xraph/nexus/providers/jinaai"
	"github.com/xraph/nexus/providers/lepton"
	"github.com/xraph/nexus/providers/lmstudio"
	"github.com/xraph/nexus/providers/mistral"
	"github.com/xraph/nexus/providers/nebius"
	"github.com/xraph/nexus/providers/novita"
	"github.com/xraph/nexus/providers/nvidia"
	"github.com/xraph/nexus/providers/ollama"
	"github.com/xraph/nexus/providers/openai"
	"github.com/xraph/nexus/providers/opencompat"
	"github.com/xraph/nexus/providers/openrouter"
	"github.com/xraph/nexus/providers/perplexity"
	"github.com/xraph/nexus/providers/sambanova"
	"github.com/xraph/nexus/providers/together"
	"github.com/xraph/nexus/providers/vertex"
	"github.com/xraph/nexus/providers/voyageai"
	"github.com/xraph/nexus/providers/xai"
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
		p := buildProvider(pc)
		if p != nil {
			opts = append(opts, nexus.WithProvider(p))
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

// buildProvider creates a provider.Provider from a ProviderConfig.
// Returns nil if the provider type is unknown.
func buildProvider(pc ProviderConfig) provider.Provider {
	switch pc.Type {
	// Original providers
	case "openai":
		var opts []openai.Option
		if pc.BaseURL != "" {
			opts = append(opts, openai.WithBaseURL(pc.BaseURL))
		}
		return openai.New(pc.APIKey, opts...)
	case "anthropic":
		var opts []anthropic.Option
		if pc.BaseURL != "" {
			opts = append(opts, anthropic.WithBaseURL(pc.BaseURL))
		}
		return anthropic.New(pc.APIKey, opts...)
	case "opencompat":
		return opencompat.New(pc.Name, pc.BaseURL, pc.APIKey)

	// OpenAI-compatible providers (Phase 1)
	case "groq":
		var opts []groq.Option
		if pc.BaseURL != "" {
			opts = append(opts, groq.WithBaseURL(pc.BaseURL))
		}
		return groq.New(pc.APIKey, opts...)
	case "together":
		var opts []together.Option
		if pc.BaseURL != "" {
			opts = append(opts, together.WithBaseURL(pc.BaseURL))
		}
		return together.New(pc.APIKey, opts...)
	case "mistral":
		var opts []mistral.Option
		if pc.BaseURL != "" {
			opts = append(opts, mistral.WithBaseURL(pc.BaseURL))
		}
		return mistral.New(pc.APIKey, opts...)
	case "deepseek":
		var opts []deepseek.Option
		if pc.BaseURL != "" {
			opts = append(opts, deepseek.WithBaseURL(pc.BaseURL))
		}
		return deepseek.New(pc.APIKey, opts...)
	case "xai":
		var opts []xai.Option
		if pc.BaseURL != "" {
			opts = append(opts, xai.WithBaseURL(pc.BaseURL))
		}
		return xai.New(pc.APIKey, opts...)
	case "openrouter":
		var opts []openrouter.Option
		if pc.BaseURL != "" {
			opts = append(opts, openrouter.WithBaseURL(pc.BaseURL))
		}
		return openrouter.New(pc.APIKey, opts...)
	case "ollama":
		var opts []ollama.Option
		if pc.BaseURL != "" {
			opts = append(opts, ollama.WithBaseURL(pc.BaseURL))
		}
		return ollama.New(opts...)
	case "lmstudio":
		var opts []lmstudio.Option
		if pc.BaseURL != "" {
			opts = append(opts, lmstudio.WithBaseURL(pc.BaseURL))
		}
		return lmstudio.New(opts...)

	// OpenAI-compatible providers (Phase 2)
	case "fireworks":
		var opts []fireworks.Option
		if pc.BaseURL != "" {
			opts = append(opts, fireworks.WithBaseURL(pc.BaseURL))
		}
		return fireworks.New(pc.APIKey, opts...)
	case "perplexity":
		var opts []perplexity.Option
		if pc.BaseURL != "" {
			opts = append(opts, perplexity.WithBaseURL(pc.BaseURL))
		}
		return perplexity.New(pc.APIKey, opts...)
	case "cerebras":
		var opts []cerebras.Option
		if pc.BaseURL != "" {
			opts = append(opts, cerebras.WithBaseURL(pc.BaseURL))
		}
		return cerebras.New(pc.APIKey, opts...)
	case "sambanova":
		var opts []sambanova.Option
		if pc.BaseURL != "" {
			opts = append(opts, sambanova.WithBaseURL(pc.BaseURL))
		}
		return sambanova.New(pc.APIKey, opts...)
	case "deepinfra":
		var opts []deepinfra.Option
		if pc.BaseURL != "" {
			opts = append(opts, deepinfra.WithBaseURL(pc.BaseURL))
		}
		return deepinfra.New(pc.APIKey, opts...)
	case "lepton":
		var opts []lepton.Option
		if pc.BaseURL != "" {
			opts = append(opts, lepton.WithBaseURL(pc.BaseURL))
		}
		return lepton.New(pc.APIKey, opts...)
	case "novita":
		var opts []novita.Option
		if pc.BaseURL != "" {
			opts = append(opts, novita.WithBaseURL(pc.BaseURL))
		}
		return novita.New(pc.APIKey, opts...)
	case "nvidia":
		var opts []nvidia.Option
		if pc.BaseURL != "" {
			opts = append(opts, nvidia.WithBaseURL(pc.BaseURL))
		}
		return nvidia.New(pc.APIKey, opts...)
	case "anyscale":
		var opts []anyscale.Option
		if pc.BaseURL != "" {
			opts = append(opts, anyscale.WithBaseURL(pc.BaseURL))
		}
		return anyscale.New(pc.APIKey, opts...)
	case "hyperbolic":
		var opts []hyperbolic.Option
		if pc.BaseURL != "" {
			opts = append(opts, hyperbolic.WithBaseURL(pc.BaseURL))
		}
		return hyperbolic.New(pc.APIKey, opts...)
	case "nebius":
		var opts []nebius.Option
		if pc.BaseURL != "" {
			opts = append(opts, nebius.WithBaseURL(pc.BaseURL))
		}
		return nebius.New(pc.APIKey, opts...)

	// Custom API format providers (Phase 3)
	case "gemini":
		var opts []gemini.Option
		if pc.BaseURL != "" {
			opts = append(opts, gemini.WithBaseURL(pc.BaseURL))
		}
		return gemini.New(pc.APIKey, opts...)
	case "cohere":
		var opts []cohere.Option
		if pc.BaseURL != "" {
			opts = append(opts, cohere.WithBaseURL(pc.BaseURL))
		}
		return cohere.New(pc.APIKey, opts...)
	case "ai21":
		var opts []ai21.Option
		if pc.BaseURL != "" {
			opts = append(opts, ai21.WithBaseURL(pc.BaseURL))
		}
		return ai21.New(pc.APIKey, opts...)
	case "bedrock":
		var opts []bedrock.Option
		if pc.BaseURL != "" {
			opts = append(opts, bedrock.WithBaseURL(pc.BaseURL))
		}
		if pc.SessionToken != "" {
			opts = append(opts, bedrock.WithSessionToken(pc.SessionToken))
		}
		return bedrock.New(pc.AccessKeyID, pc.SecretAccessKey, pc.Region, opts...)

	// Cloud-hosted providers (Phase 4)
	case "azureopenai":
		var opts []azureopenai.Option
		if pc.ResourceName != "" {
			opts = append(opts, azureopenai.WithResourceName(pc.ResourceName))
		}
		if pc.DeploymentID != "" {
			opts = append(opts, azureopenai.WithDeploymentID(pc.DeploymentID))
		}
		if pc.APIVersion != "" {
			opts = append(opts, azureopenai.WithAPIVersion(pc.APIVersion))
		}
		if pc.BaseURL != "" {
			opts = append(opts, azureopenai.WithBaseURL(pc.BaseURL))
		}
		return azureopenai.New(pc.APIKey, opts...)
	case "vertex":
		var opts []vertex.Option
		if pc.ProjectID != "" {
			opts = append(opts, vertex.WithProjectID(pc.ProjectID))
		}
		if pc.Location != "" {
			opts = append(opts, vertex.WithLocation(pc.Location))
		}
		if pc.AccessToken != "" {
			opts = append(opts, vertex.WithAccessToken(pc.AccessToken))
		}
		if pc.CredentialFile != "" {
			data, _ := os.ReadFile(pc.CredentialFile)
			if len(data) > 0 {
				opts = append(opts, vertex.WithCredentialsJSON(data))
			}
		}
		if pc.BaseURL != "" {
			opts = append(opts, vertex.WithBaseURL(pc.BaseURL))
		}
		return vertex.New(opts...)

	// Embeddings-only providers (Phase 4)
	case "voyageai":
		var opts []voyageai.Option
		if pc.BaseURL != "" {
			opts = append(opts, voyageai.WithBaseURL(pc.BaseURL))
		}
		return voyageai.New(pc.APIKey, opts...)
	case "jinaai":
		var opts []jinaai.Option
		if pc.BaseURL != "" {
			opts = append(opts, jinaai.WithBaseURL(pc.BaseURL))
		}
		return jinaai.New(pc.APIKey, opts...)

	default:
		return nil
	}
}

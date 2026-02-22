package middlewares

import (
	"context"
	"math/rand"

	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/pipeline"
)

// AliasMiddleware resolves model aliases before the request reaches the provider.
// If the requested model is an alias (e.g., "fast", "smart", "cheap"), it
// resolves it to a concrete provider+model target.
type AliasMiddleware struct {
	aliases model.AliasRegistry
}

// NewAlias creates a model alias resolution middleware.
func NewAlias(aliases model.AliasRegistry) *AliasMiddleware {
	return &AliasMiddleware{aliases: aliases}
}

func (m *AliasMiddleware) Name() string  { return "alias" }
func (m *AliasMiddleware) Priority() int { return 250 } // Before routing (350)

func (m *AliasMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.aliases == nil || req.Completion == nil {
		return next(ctx)
	}

	tenantID := pipeline.TenantID(ctx)
	targets, err := m.aliases.Resolve(ctx, req.Completion.Model, tenantID)
	if err != nil || len(targets) == 0 {
		// Not an alias — continue with original model name
		return next(ctx)
	}

	// Select a target based on weights
	target := selectTarget(targets)
	if target == nil {
		return next(ctx)
	}

	// Store original model name for logging/metrics
	req.State["original_model"] = req.Completion.Model
	req.State["alias_target_provider"] = target.Provider
	req.State["alias_target_model"] = target.Model

	// Rewrite the model
	req.Completion.Model = target.Model

	return next(ctx)
}

// selectTarget picks a target based on weight. If no weights, uniform random.
func selectTarget(targets []model.AliasTarget) *model.AliasTarget {
	if len(targets) == 0 {
		return nil
	}
	if len(targets) == 1 {
		return &targets[0]
	}

	// Calculate total weight
	var totalWeight float64
	for _, t := range targets {
		w := t.Weight
		if w <= 0 {
			w = 1.0
		}
		totalWeight += w
	}

	// Weighted random selection
	r := rand.Float64() * totalWeight
	for i := range targets {
		w := targets[i].Weight
		if w <= 0 {
			w = 1.0
		}
		r -= w
		if r <= 0 {
			return &targets[i]
		}
	}

	return &targets[len(targets)-1]
}

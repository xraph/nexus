// Package model defines model alias resolution and model metadata services.
package model

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// Service combines alias resolution and model catalog.
type Service interface {
	// ListModels returns all available models across providers.
	ListModels(ctx context.Context) ([]provider.Model, error)

	// Get returns model metadata by ID.
	Get(ctx context.Context, modelID string) (*provider.Model, error)

	// ResolveAlias resolves a model alias to a concrete model target.
	// Returns the original modelID if no alias exists.
	ResolveAlias(ctx context.Context, alias string, tenantID string) (string, string, error)
}

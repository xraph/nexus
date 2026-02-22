// Package strategies provides built-in routing strategies for Nexus.
package strategies

import (
	"context"
	"errors"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
)

// PriorityStrategy selects providers in priority order (first healthy wins).
type PriorityStrategy struct {
	order []string
}

// NewPriority creates a priority-based routing strategy.
// Providers are tried in the given order; first healthy provider wins.
func NewPriority(order ...string) *PriorityStrategy {
	return &PriorityStrategy{order: order}
}

func (s *PriorityStrategy) Name() string { return "priority" }

func (s *PriorityStrategy) Select(ctx context.Context, req *provider.CompletionRequest, candidates []router.Candidate) (*router.Candidate, error) {
	// Build lookup by name
	byName := make(map[string]*router.Candidate, len(candidates))
	for i := range candidates {
		byName[candidates[i].Provider.Name()] = &candidates[i]
	}

	// Try in priority order
	for _, name := range s.order {
		if c, ok := byName[name]; ok && c.Healthy {
			return c, nil
		}
	}

	// Fall through: return first healthy candidate
	for i := range candidates {
		if candidates[i].Healthy {
			return &candidates[i], nil
		}
	}

	return nil, errors.New("nexus: no healthy providers available")
}

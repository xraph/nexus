package strategies

import (
	"context"
	"errors"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
)

// CostOptimizedStrategy selects the cheapest provider.
type CostOptimizedStrategy struct{}

// NewCostOptimized creates a cost-optimized routing strategy.
func NewCostOptimized() *CostOptimizedStrategy {
	return &CostOptimizedStrategy{}
}

func (s *CostOptimizedStrategy) Name() string { return "cost_optimized" }

func (s *CostOptimizedStrategy) Select(_ context.Context, _ *provider.CompletionRequest, candidates []router.Candidate) (*router.Candidate, error) {
	var best *router.Candidate
	for i := range candidates {
		c := &candidates[i]
		if !c.Healthy {
			continue
		}
		if best == nil || c.Cost < best.Cost {
			best = c
		}
	}

	if best == nil {
		return nil, errors.New("nexus: no healthy providers available")
	}
	return best, nil
}

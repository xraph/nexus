package strategies

import (
	"context"
	"errors"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
)

// LatencyOptimizedStrategy selects the fastest provider.
type LatencyOptimizedStrategy struct{}

// NewLatencyOptimized creates a latency-optimized routing strategy.
func NewLatencyOptimized() *LatencyOptimizedStrategy {
	return &LatencyOptimizedStrategy{}
}

func (s *LatencyOptimizedStrategy) Name() string { return "latency_optimized" }

func (s *LatencyOptimizedStrategy) Select(_ context.Context, _ *provider.CompletionRequest, candidates []router.Candidate) (*router.Candidate, error) {
	var best *router.Candidate
	for i := range candidates {
		c := &candidates[i]
		if !c.Healthy {
			continue
		}
		if best == nil || (c.Latency > 0 && c.Latency < best.Latency) {
			best = c
		}
	}

	if best == nil {
		return nil, errors.New("nexus: no healthy providers available")
	}
	return best, nil
}

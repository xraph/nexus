package strategies

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
)

// RoundRobinStrategy distributes requests evenly across providers.
type RoundRobinStrategy struct {
	counter atomic.Uint64
}

// NewRoundRobin creates a round-robin routing strategy.
func NewRoundRobin() *RoundRobinStrategy {
	return &RoundRobinStrategy{}
}

func (s *RoundRobinStrategy) Name() string { return "round_robin" }

func (s *RoundRobinStrategy) Select(ctx context.Context, req *provider.CompletionRequest, candidates []router.Candidate) (*router.Candidate, error) {
	healthy := make([]*router.Candidate, 0, len(candidates))
	for i := range candidates {
		if candidates[i].Healthy {
			healthy = append(healthy, &candidates[i])
		}
	}

	if len(healthy) == 0 {
		return nil, errors.New("nexus: no healthy providers available")
	}

	idx := s.counter.Add(1) - 1
	return healthy[idx%uint64(len(healthy))], nil
}

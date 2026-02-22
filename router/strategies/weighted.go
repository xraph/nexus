package strategies

import (
	"context"
	"errors"
	"math/rand"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
)

// WeightedStrategy distributes requests by weight.
type WeightedStrategy struct {
	weights map[string]float64
}

// NewWeighted creates a weighted routing strategy.
// The weights map provider names to their relative weights.
func NewWeighted(weights map[string]float64) *WeightedStrategy {
	return &WeightedStrategy{weights: weights}
}

func (s *WeightedStrategy) Name() string { return "weighted" }

func (s *WeightedStrategy) Select(ctx context.Context, req *provider.CompletionRequest, candidates []router.Candidate) (*router.Candidate, error) {
	// Assign weights to candidates
	type weighted struct {
		candidate *router.Candidate
		weight    float64
	}

	var items []weighted
	var totalWeight float64

	for i := range candidates {
		if !candidates[i].Healthy {
			continue
		}
		w := s.weights[candidates[i].Provider.Name()]
		if w <= 0 {
			w = 1.0 // default weight
		}
		items = append(items, weighted{candidate: &candidates[i], weight: w})
		totalWeight += w
	}

	if len(items) == 0 {
		return nil, errors.New("nexus: no healthy providers available")
	}

	// Weighted random selection
	r := rand.Float64() * totalWeight
	for _, item := range items {
		r -= item.weight
		if r <= 0 {
			return item.candidate, nil
		}
	}

	return items[len(items)-1].candidate, nil
}

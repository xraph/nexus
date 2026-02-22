// Package router defines the routing engine that selects the best provider
// for a request.
package router

import (
	"context"
	"time"

	"github.com/xraph/nexus/provider"
)

// Service selects the best provider for a request.
type Service interface {
	// Route selects a provider for the given request.
	Route(ctx context.Context, req *provider.CompletionRequest, providers []provider.Provider) (provider.Provider, error)
}

// Strategy defines a routing algorithm.
type Strategy interface {
	Name() string
	Select(ctx context.Context, req *provider.CompletionRequest, candidates []Candidate) (*Candidate, error)
}

// Candidate is a provider eligible for routing.
type Candidate struct {
	Provider provider.Provider
	Weight   float64 // for weighted strategies
	Priority int     // for priority strategies
	Healthy  bool
	Latency  time.Duration // recent average
	Cost     float64       // per-token cost
}

// NewService creates a router Service from a Strategy.
func NewService(strategy Strategy) Service {
	return &routerService{strategy: strategy}
}

type routerService struct {
	strategy Strategy
}

func (r *routerService) Route(ctx context.Context, req *provider.CompletionRequest, providers []provider.Provider) (provider.Provider, error) {
	candidates := make([]Candidate, len(providers))
	for i, p := range providers {
		candidates[i] = Candidate{
			Provider: p,
			Healthy:  true,
		}
	}

	selected, err := r.strategy.Select(ctx, req, candidates)
	if err != nil {
		return nil, err
	}
	return selected.Provider, nil
}

package provider

import (
	"context"
	"sync"
)

// Registry manages registered LLM providers.
type Registry interface {
	// Register adds a provider to the registry.
	Register(p Provider)

	// Get returns a provider by name.
	Get(name string) (Provider, bool)

	// All returns all registered providers.
	All() []Provider

	// Count returns the number of registered providers.
	Count() int

	// ForModel returns providers that support a given model.
	ForModel(model string) []Provider

	// WithCapability returns providers with a specific capability.
	WithCapability(capability string) []Provider

	// Healthy returns only healthy providers.
	Healthy(ctx context.Context) []Provider
}

type registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	order     []string // preserve registration order
}

// NewRegistry creates an empty provider registry.
func NewRegistry() Registry {
	return &registry{
		providers: make(map[string]Provider),
	}
}

func (r *registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := p.Name()
	if _, exists := r.providers[name]; !exists {
		r.order = append(r.order, name)
	}
	r.providers[name] = p
}

func (r *registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

func (r *registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Provider, 0, len(r.order))
	for _, name := range r.order {
		result = append(result, r.providers[name])
	}
	return result
}

func (r *registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.providers)
}

func (r *registry) ForModel(_ string) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Provider, 0, len(r.order))
	for _, name := range r.order {
		p := r.providers[name]
		// Check if provider supports the model by querying its model list
		// For now, return all providers (actual filtering requires model catalog)
		_ = p
		result = append(result, p)
	}
	return result
}

func (r *registry) WithCapability(capability string) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Provider
	for _, name := range r.order {
		p := r.providers[name]
		if p.Capabilities().Supports(capability) {
			result = append(result, p)
		}
	}
	return result
}

func (r *registry) Healthy(ctx context.Context) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Provider
	for _, name := range r.order {
		p := r.providers[name]
		if p.Healthy(ctx) {
			result = append(result, p)
		}
	}
	return result
}

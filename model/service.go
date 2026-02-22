package model

import (
	"context"
	"sync"

	"github.com/xraph/nexus/provider"
)

// defaultService combines alias registry, model catalog, and provider registry.
type defaultService struct {
	aliases   AliasRegistry
	providers provider.Registry
	mu        sync.RWMutex
	catalog   map[string]*provider.Model // model ID → model metadata
}

// NewService creates a model service with the given alias registry and provider registry.
func NewService(aliases AliasRegistry, providers provider.Registry) Service {
	return &defaultService{
		aliases:   aliases,
		providers: providers,
		catalog:   make(map[string]*provider.Model),
	}
}

func (s *defaultService) ListModels(ctx context.Context) ([]provider.Model, error) {
	var models []provider.Model
	for _, p := range s.providers.All() {
		m, err := p.Models(ctx)
		if err != nil {
			continue
		}
		models = append(models, m...)
	}
	return models, nil
}

func (s *defaultService) Get(ctx context.Context, modelID string) (*provider.Model, error) {
	s.mu.RLock()
	if m, ok := s.catalog[modelID]; ok {
		s.mu.RUnlock()
		return m, nil
	}
	s.mu.RUnlock()

	// Try to find from providers
	for _, p := range s.providers.All() {
		models, err := p.Models(ctx)
		if err != nil {
			continue
		}
		for _, m := range models {
			s.mu.Lock()
			mc := m // copy for pointer
			s.catalog[m.ID] = &mc
			s.mu.Unlock()
			if m.ID == modelID {
				return &mc, nil
			}
		}
	}

	return nil, nil
}

func (s *defaultService) ResolveAlias(ctx context.Context, alias string, tenantID string) (string, string, error) {
	if s.aliases == nil {
		return alias, "", nil
	}

	targets, err := s.aliases.Resolve(ctx, alias, tenantID)
	if err != nil {
		return "", "", err
	}

	if targets == nil || len(targets) == 0 {
		// Not an alias — return as concrete model
		return alias, "", nil
	}

	// Return the first target (priority-based)
	return targets[0].Model, targets[0].Provider, nil
}

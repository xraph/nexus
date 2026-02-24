package model

import (
	"context"
	"sync"
)

// Alias maps a virtual model name to one or more concrete models.
type Alias struct {
	Name            string                   `json:"name"`    // "fast", "smart", "code", "embed"
	Targets         []AliasTarget            `json:"targets"` // ordered fallback chain
	TenantOverrides map[string][]AliasTarget `json:"tenant_overrides,omitempty"`
}

// AliasTarget maps to a specific provider + model.
type AliasTarget struct {
	Provider string  `json:"provider"`         // "openai", "anthropic"
	Model    string  `json:"model"`            // "gpt-4o-mini", "claude-3.5-haiku"
	Weight   float64 `json:"weight,omitempty"` // for weighted distribution
}

// AliasRegistry resolves aliases at request time.
type AliasRegistry interface {
	// Resolve returns the concrete targets for a model name.
	// If the name is already concrete, returns it as-is.
	Resolve(ctx context.Context, name, tenantID string) ([]AliasTarget, error)

	// Register adds or updates an alias.
	Register(alias *Alias) error

	// List returns all registered aliases.
	List() []Alias
}

// inMemoryAliasRegistry is a thread-safe in-memory alias registry.
type inMemoryAliasRegistry struct {
	mu      sync.RWMutex
	aliases map[string]*Alias
}

// NewAliasRegistry creates an in-memory alias registry.
func NewAliasRegistry() AliasRegistry {
	return &inMemoryAliasRegistry{
		aliases: make(map[string]*Alias),
	}
}

func (r *inMemoryAliasRegistry) Resolve(_ context.Context, name, tenantID string) ([]AliasTarget, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	alias, ok := r.aliases[name]
	if !ok {
		// Not an alias — return as concrete model
		return nil, nil
	}

	// Check for tenant-specific overrides first
	if tenantID != "" && alias.TenantOverrides != nil {
		if targets, ok := alias.TenantOverrides[tenantID]; ok {
			return targets, nil
		}
	}

	return alias.Targets, nil
}

func (r *inMemoryAliasRegistry) Register(alias *Alias) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[alias.Name] = alias
	return nil
}

func (r *inMemoryAliasRegistry) List() []Alias {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Alias, 0, len(r.aliases))
	for _, a := range r.aliases {
		result = append(result, *a)
	}
	return result
}

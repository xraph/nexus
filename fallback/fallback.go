// Package fallback defines the resilience and fallback subsystem.
package fallback

import (
	"context"
	"time"

	"github.com/xraph/nexus/provider"
)

// Service manages provider-level resilience.
type Service interface {
	// Execute calls the provider with retry + circuit breaker + fallback.
	Execute(ctx context.Context, primary provider.Provider, fallbacks []provider.Provider, req *provider.CompletionRequest) (*provider.CompletionResponse, error)

	// CircuitState returns the circuit breaker state for a provider.
	CircuitState(providerName string) State

	// Reset resets the circuit breaker for a provider.
	Reset(providerName string)
}

// State represents a circuit breaker state.
type State string

const (
	StateClosed   State = "closed"    // normal operation
	StateOpen     State = "open"      // all requests fail fast
	StateHalfOpen State = "half-open" // testing recovery
)

// Policy defines retry and fallback behavior.
type Policy struct {
	MaxRetries       int           `json:"max_retries"`       // per provider
	RetryDelay       time.Duration `json:"retry_delay"`       // initial delay
	RetryBackoff     float64       `json:"retry_backoff"`     // multiplier (e.g., 2.0)
	Timeout          time.Duration `json:"timeout"`           // per-request timeout
	CircuitThreshold int           `json:"circuit_threshold"` // failures before open
	CircuitTimeout   time.Duration `json:"circuit_timeout"`   // open duration
}

// DefaultPolicy returns sensible defaults.
func DefaultPolicy() *Policy {
	return &Policy{
		MaxRetries:       2,
		RetryDelay:       500 * time.Millisecond,
		RetryBackoff:     2.0,
		Timeout:          30 * time.Second,
		CircuitThreshold: 5,
		CircuitTimeout:   30 * time.Second,
	}
}

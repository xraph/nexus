// Package fallback defines the resilience and fallback subsystem.
//
// The service calls providers directly, outside the gateway pipeline, so it
// bypasses credential resolution (nexus.WithCredentialProvider): every call
// it makes uses each provider's configured key and base URL, whatever tenant
// the request belongs to. Route tenant traffic through the gateway's Engine
// instead.
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

	// ExecuteStream is the streaming counterpart to Execute. Primary and
	// each fallback are tried in turn; the first one whose CompleteStream
	// returns successfully wins. Mid-stream failures (errors surfaced from
	// Stream.Next after a chunk has been delivered) are NOT retried — the
	// consumer has already begun receiving data, and replaying upstream
	// would duplicate content.
	ExecuteStream(ctx context.Context, primary provider.Provider, fallbacks []provider.Provider, req *provider.CompletionRequest) (provider.Stream, error)

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

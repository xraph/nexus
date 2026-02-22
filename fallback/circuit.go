package fallback

import (
	"sync"
	"time"
)

// CircuitBreaker implements the circuit breaker pattern per provider.
type CircuitBreaker struct {
	mu        sync.RWMutex
	state     State
	failures  int
	threshold int
	timeout   time.Duration
	openedAt  time.Time
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:     StateClosed,
		threshold: threshold,
		timeout:   timeout,
	}
}

// Allow returns true if the request should proceed.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.openedAt) >= cb.timeout {
			return true // transition to half-open
		}
		return false
	case StateHalfOpen:
		return true // allow one test request
	default:
		return true
	}
}

// RecordSuccess marks a successful call.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = StateClosed
}

// RecordFailure marks a failed call and may trip the circuit.
func (cb *CircuitBreaker) RecordFailure() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	if cb.failures >= cb.threshold {
		cb.state = StateOpen
		cb.openedAt = time.Now()
		return true // circuit just opened
	}
	return false
}

// State returns the current state.
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.state == StateOpen && time.Since(cb.openedAt) >= cb.timeout {
		return StateHalfOpen
	}
	return cb.state
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.failures = 0
}

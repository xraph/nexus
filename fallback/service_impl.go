package fallback

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/nexus/provider"
)

type service struct {
	policy   *Policy
	mu       sync.RWMutex
	circuits map[string]*CircuitBreaker
}

// NewService creates a new fallback service with the given policy.
func NewService(policy *Policy) Service {
	if policy == nil {
		policy = DefaultPolicy()
	}
	return &service{
		policy:   policy,
		circuits: make(map[string]*CircuitBreaker),
	}
}

func (s *service) Execute(ctx context.Context, primary provider.Provider, fallbacks []provider.Provider, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	// Try primary with retries
	resp, err := s.tryWithRetries(ctx, primary, req)
	if err == nil {
		return resp, nil
	}

	// Primary failed — try fallbacks
	for _, fb := range fallbacks {
		resp, err = s.tryWithRetries(ctx, fb, req)
		if err == nil {
			return resp, nil
		}
	}

	return nil, fmt.Errorf("nexus: all providers failed, last error: %w", err)
}

func (s *service) tryWithRetries(ctx context.Context, p provider.Provider, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	cb := s.getCircuit(p.Name())

	if !cb.Allow() {
		return nil, fmt.Errorf("nexus: circuit open for %s", p.Name())
	}

	var lastErr error
	delay := s.policy.RetryDelay

	for attempt := 0; attempt <= s.policy.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				delay = time.Duration(float64(delay) * s.policy.RetryBackoff)
			}
		}

		// Per-request timeout
		callCtx, cancel := context.WithTimeout(ctx, s.policy.Timeout)
		resp, err := p.Complete(callCtx, req)
		cancel()

		if err == nil {
			cb.RecordSuccess()
			return resp, nil
		}

		lastErr = err
	}

	// All retries exhausted
	cb.RecordFailure()
	return nil, lastErr
}

func (s *service) getCircuit(name string) *CircuitBreaker {
	s.mu.RLock()
	cb, ok := s.circuits[name]
	s.mu.RUnlock()

	if ok {
		return cb
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if existingCB, ok := s.circuits[name]; ok {
		return existingCB
	}

	cb = NewCircuitBreaker(s.policy.CircuitThreshold, s.policy.CircuitTimeout)
	s.circuits[name] = cb
	return cb
}

func (s *service) CircuitState(providerName string) State {
	s.mu.RLock()
	cb, ok := s.circuits[providerName]
	s.mu.RUnlock()

	if !ok {
		return StateClosed
	}
	return cb.State()
}

func (s *service) Reset(providerName string) {
	s.mu.RLock()
	cb, ok := s.circuits[providerName]
	s.mu.RUnlock()

	if ok {
		cb.Reset()
	}
}

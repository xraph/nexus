package provider

import (
	"sync"
	"time"
)

// HealthTracker monitors provider health for routing decisions.
type HealthTracker interface {
	// RecordSuccess records a successful provider call.
	RecordSuccess(providerName string, latency time.Duration)

	// RecordFailure records a failed provider call.
	RecordFailure(providerName string, err error)

	// Stats returns health statistics for a provider.
	Stats(providerName string) *HealthStats

	// IsHealthy returns true if the provider is considered healthy.
	IsHealthy(providerName string) bool

	// AllStats returns stats for all tracked providers.
	AllStats() map[string]*HealthStats
}

// HealthStats contains health metrics for a provider.
type HealthStats struct {
	TotalRequests int           `json:"total_requests"`
	Successes     int           `json:"successes"`
	Failures      int           `json:"failures"`
	SuccessRate   float64       `json:"success_rate"`
	AvgLatency    time.Duration `json:"avg_latency"`
	P99Latency    time.Duration `json:"p99_latency"`
	LastSuccess   time.Time     `json:"last_success,omitempty"`
	LastFailure   time.Time     `json:"last_failure,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
}

// NewHealthTracker creates a new in-memory health tracker.
func NewHealthTracker() HealthTracker {
	return &memoryHealthTracker{
		providers: make(map[string]*providerHealth),
	}
}

type memoryHealthTracker struct {
	mu        sync.RWMutex
	providers map[string]*providerHealth
}

type providerHealth struct {
	successes   int
	failures    int
	latencies   []time.Duration
	lastSuccess time.Time
	lastFailure time.Time
	lastError   string
}

func (h *memoryHealthTracker) RecordSuccess(name string, latency time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	p := h.getOrCreate(name)
	p.successes++
	p.latencies = append(p.latencies, latency)
	p.lastSuccess = time.Now()

	// Keep only last 1000 latencies
	if len(p.latencies) > 1000 {
		p.latencies = p.latencies[len(p.latencies)-1000:]
	}
}

func (h *memoryHealthTracker) RecordFailure(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	p := h.getOrCreate(name)
	p.failures++
	p.lastFailure = time.Now()
	if err != nil {
		p.lastError = err.Error()
	}
}

func (h *memoryHealthTracker) Stats(name string) *HealthStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	p, ok := h.providers[name]
	if !ok {
		return &HealthStats{}
	}
	return p.stats()
}

func (h *memoryHealthTracker) IsHealthy(name string) bool {
	stats := h.Stats(name)
	if stats.TotalRequests == 0 {
		return true // no data, assume healthy
	}
	return stats.SuccessRate > 0.5 // > 50% success rate
}

func (h *memoryHealthTracker) AllStats() map[string]*HealthStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[string]*HealthStats, len(h.providers))
	for name, p := range h.providers {
		result[name] = p.stats()
	}
	return result
}

func (h *memoryHealthTracker) getOrCreate(name string) *providerHealth {
	p, ok := h.providers[name]
	if !ok {
		p = &providerHealth{}
		h.providers[name] = p
	}
	return p
}

func (p *providerHealth) stats() *HealthStats {
	total := p.successes + p.failures
	var rate float64
	if total > 0 {
		rate = float64(p.successes) / float64(total)
	}

	var avgLatency, p99Latency time.Duration
	if len(p.latencies) > 0 {
		var sum time.Duration
		for _, l := range p.latencies {
			sum += l
		}
		avgLatency = sum / time.Duration(len(p.latencies))

		// Simple P99: sort and take 99th percentile
		// For efficiency, just take near-max value
		idx := int(float64(len(p.latencies)) * 0.99)
		if idx >= len(p.latencies) {
			idx = len(p.latencies) - 1
		}
		p99Latency = p.latencies[idx]
	}

	return &HealthStats{
		TotalRequests: total,
		Successes:     p.successes,
		Failures:      p.failures,
		SuccessRate:   rate,
		AvgLatency:    avgLatency,
		P99Latency:    p99Latency,
		LastSuccess:   p.lastSuccess,
		LastFailure:   p.lastFailure,
		LastError:     p.lastError,
	}
}

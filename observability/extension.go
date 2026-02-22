package observability

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/plugin"
)

// Counter is a simple atomic counter for metrics.
// In a full integration, this would be backed by gu.Counter or Prometheus.
type Counter struct {
	value atomic.Int64
}

// Inc increments the counter.
func (c *Counter) Inc() { c.value.Add(1) }

// Add increments the counter by n.
func (c *Counter) Add(n int64) { c.value.Add(n) }

// Value returns the current count.
func (c *Counter) Value() int64 { return c.value.Load() }

// MetricsExtension tracks operational metrics via counters.
// Follows the dispatch/observability pattern.
type MetricsExtension struct {
	RequestsReceived  Counter
	RequestsCompleted Counter
	RequestsFailed    Counter
	RequestsCached    Counter

	ProviderFailures Counter
	CircuitOpens     Counter
	Fallbacks        Counter

	GuardrailBlocks  Counter
	GuardrailRedacts Counter

	TenantsCreated  Counter
	TenantsDisabled Counter
	KeysCreated     Counter
	KeysRevoked     Counter

	BudgetWarnings Counter
	BudgetExceeded Counter
}

// NewMetricsExtension creates a new metrics extension.
func NewMetricsExtension() *MetricsExtension {
	return &MetricsExtension{}
}

func (e *MetricsExtension) Name() string { return "observability" }

// Request lifecycle hooks

func (e *MetricsExtension) OnRequestReceived(_ context.Context, _ id.RequestID, _ string, _ string) error {
	e.RequestsReceived.Inc()
	return nil
}

func (e *MetricsExtension) OnRequestCompleted(_ context.Context, _ id.RequestID, _ string, _ string, _ time.Duration, _ int, _ int) error {
	e.RequestsCompleted.Inc()
	return nil
}

func (e *MetricsExtension) OnRequestFailed(_ context.Context, _ id.RequestID, _ string, _ error) error {
	e.RequestsFailed.Inc()
	return nil
}

func (e *MetricsExtension) OnRequestCached(_ context.Context, _ id.RequestID, _ string) error {
	e.RequestsCached.Inc()
	return nil
}

// Provider lifecycle hooks

func (e *MetricsExtension) OnProviderFailed(_ context.Context, _ string, _ string, _ error) error {
	e.ProviderFailures.Inc()
	return nil
}

func (e *MetricsExtension) OnCircuitOpened(_ context.Context, _ string) error {
	e.CircuitOpens.Inc()
	return nil
}

func (e *MetricsExtension) OnFallbackTriggered(_ context.Context, _ string, _ string) error {
	e.Fallbacks.Inc()
	return nil
}

// Guardrail lifecycle hooks

func (e *MetricsExtension) OnGuardrailBlocked(_ context.Context, _ string, _ id.RequestID) error {
	e.GuardrailBlocks.Inc()
	return nil
}

func (e *MetricsExtension) OnGuardrailRedacted(_ context.Context, _ string, _ string) error {
	e.GuardrailRedacts.Inc()
	return nil
}

// Tenant/Key lifecycle hooks

func (e *MetricsExtension) OnTenantCreated(_ context.Context, _ id.TenantID) error {
	e.TenantsCreated.Inc()
	return nil
}

func (e *MetricsExtension) OnTenantDisabled(_ context.Context, _ id.TenantID) error {
	e.TenantsDisabled.Inc()
	return nil
}

func (e *MetricsExtension) OnKeyCreated(_ context.Context, _ id.KeyID, _ id.TenantID) error {
	e.KeysCreated.Inc()
	return nil
}

func (e *MetricsExtension) OnKeyRevoked(_ context.Context, _ id.KeyID) error {
	e.KeysRevoked.Inc()
	return nil
}

// Budget lifecycle hooks

func (e *MetricsExtension) OnBudgetWarning(_ context.Context, _ id.TenantID, _ float64) error {
	e.BudgetWarnings.Inc()
	return nil
}

func (e *MetricsExtension) OnBudgetExceeded(_ context.Context, _ id.TenantID) error {
	e.BudgetExceeded.Inc()
	return nil
}

// Compile-time interface checks.
var (
	_ plugin.Extension         = (*MetricsExtension)(nil)
	_ plugin.RequestReceived   = (*MetricsExtension)(nil)
	_ plugin.RequestCompleted  = (*MetricsExtension)(nil)
	_ plugin.RequestFailed     = (*MetricsExtension)(nil)
	_ plugin.RequestCached     = (*MetricsExtension)(nil)
	_ plugin.ProviderFailed    = (*MetricsExtension)(nil)
	_ plugin.CircuitOpened     = (*MetricsExtension)(nil)
	_ plugin.FallbackTriggered = (*MetricsExtension)(nil)
	_ plugin.GuardrailBlocked  = (*MetricsExtension)(nil)
	_ plugin.GuardrailRedacted = (*MetricsExtension)(nil)
	_ plugin.TenantCreated     = (*MetricsExtension)(nil)
	_ plugin.TenantDisabled    = (*MetricsExtension)(nil)
	_ plugin.KeyCreated        = (*MetricsExtension)(nil)
	_ plugin.KeyRevoked        = (*MetricsExtension)(nil)
	_ plugin.BudgetWarning     = (*MetricsExtension)(nil)
	_ plugin.BudgetExceeded    = (*MetricsExtension)(nil)
)

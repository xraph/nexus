// Package plugin defines the extension system for Nexus.
// Extensions are notified of lifecycle events (request received, completed,
// failed, cached, etc.) and can react to them — audit logging, metrics,
// tracing, webhooks, etc.
//
// Each lifecycle hook is a separate interface so extensions opt in only
// to the events they care about.
package plugin

import (
	"context"
	"time"

	"github.com/xraph/nexus/id"
)

// Extension is the base interface all extensions must implement.
type Extension interface {
	// Name returns a unique human-readable name for the extension.
	Name() string
}

// ──────────────────────────────────────────────────
// Request lifecycle hooks
// ──────────────────────────────────────────────────

// RequestReceived is called when a request enters the pipeline.
type RequestReceived interface {
	OnRequestReceived(ctx context.Context, requestID id.RequestID, model string, tenantID string) error
}

// RequestCompleted is called after a successful provider response.
type RequestCompleted interface {
	OnRequestCompleted(ctx context.Context, requestID id.RequestID, model string, providerName string, elapsed time.Duration, inputTokens int, outputTokens int) error
}

// RequestFailed is called when a request fails.
type RequestFailed interface {
	OnRequestFailed(ctx context.Context, requestID id.RequestID, model string, err error) error
}

// RequestCached is called when a response is served from cache.
type RequestCached interface {
	OnRequestCached(ctx context.Context, requestID id.RequestID, model string) error
}

// ──────────────────────────────────────────────────
// Provider lifecycle hooks
// ──────────────────────────────────────────────────

// ProviderFailed is called when a provider call fails (before retry/fallback).
type ProviderFailed interface {
	OnProviderFailed(ctx context.Context, providerName string, model string, err error) error
}

// CircuitOpened is called when a circuit breaker opens for a provider.
type CircuitOpened interface {
	OnCircuitOpened(ctx context.Context, providerName string) error
}

// FallbackTriggered is called when a fallback provider is used.
type FallbackTriggered interface {
	OnFallbackTriggered(ctx context.Context, from string, to string) error
}

// ──────────────────────────────────────────────────
// Guardrail lifecycle hooks
// ──────────────────────────────────────────────────

// GuardrailBlocked is called when a guardrail blocks a request.
type GuardrailBlocked interface {
	OnGuardrailBlocked(ctx context.Context, guardName string, requestID id.RequestID) error
}

// GuardrailRedacted is called when a guardrail redacts content.
type GuardrailRedacted interface {
	OnGuardrailRedacted(ctx context.Context, guardName string, field string) error
}

// ──────────────────────────────────────────────────
// Tenant & key lifecycle hooks
// ──────────────────────────────────────────────────

// TenantCreated is called when a new tenant is created.
type TenantCreated interface {
	OnTenantCreated(ctx context.Context, tenantID id.TenantID) error
}

// TenantDisabled is called when a tenant is disabled.
type TenantDisabled interface {
	OnTenantDisabled(ctx context.Context, tenantID id.TenantID) error
}

// KeyCreated is called when an API key is created.
type KeyCreated interface {
	OnKeyCreated(ctx context.Context, keyID id.KeyID, tenantID id.TenantID) error
}

// KeyRevoked is called when an API key is revoked.
type KeyRevoked interface {
	OnKeyRevoked(ctx context.Context, keyID id.KeyID) error
}

// ──────────────────────────────────────────────────
// Budget lifecycle hooks
// ──────────────────────────────────────────────────

// BudgetWarning is called at 80% budget threshold.
type BudgetWarning interface {
	OnBudgetWarning(ctx context.Context, tenantID id.TenantID, usedPct float64) error
}

// BudgetExceeded is called when budget is exceeded.
type BudgetExceeded interface {
	OnBudgetExceeded(ctx context.Context, tenantID id.TenantID) error
}

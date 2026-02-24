// Package audit_hook provides the audit extension for Nexus.
// It bridges lifecycle events from the plugin.Registry to an audit recorder
// (typically Chronicle). This follows the dispatch/audit_hook pattern exactly.
//
// Usage:
//
//	nexus.WithExtension(audithook.New(audithook.RecorderFunc(func(ctx context.Context, event AuditEvent) error {
//	    log.Println("audit:", event.Action, event.Resource)
//	    return nil
//	})))
package audit_hook

import (
	"context"
	"log/slog"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/plugin"
)

// Recorder persists audit events.
type Recorder interface {
	Record(ctx context.Context, event AuditEvent) error
}

// RecorderFunc is a function adapter for Recorder.
type RecorderFunc func(ctx context.Context, event AuditEvent) error

func (f RecorderFunc) Record(ctx context.Context, event AuditEvent) error { return f(ctx, event) }

// AuditEvent is an audit log entry.
type AuditEvent struct {
	Timestamp  time.Time      `json:"timestamp"`
	Action     Action         `json:"action"`
	Resource   Resource       `json:"resource"`
	Category   Category       `json:"category"`
	ActorID    string         `json:"actor_id,omitempty"` // tenant or key ID
	ResourceID string         `json:"resource_id,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// Extension implements all plugin lifecycle hooks and records audit events.
type Extension struct {
	recorder Recorder
	logger   *slog.Logger
	actions  map[Action]bool // nil = all actions
}

// Option configures the audit extension.
type Option func(*Extension)

// WithActions filters to only record specific actions.
func WithActions(actions ...Action) Option {
	return func(e *Extension) {
		e.actions = make(map[Action]bool, len(actions))
		for _, a := range actions {
			e.actions[a] = true
		}
	}
}

// WithLogger sets a custom logger for the extension.
func WithLogger(l *slog.Logger) Option {
	return func(e *Extension) { e.logger = l }
}

// New creates a new audit hook extension.
func New(recorder Recorder, opts ...Option) *Extension {
	e := &Extension{
		recorder: recorder,
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

func (e *Extension) Name() string { return "audit_hook" }

// record is the internal helper that creates and records an audit event.
func (e *Extension) record(ctx context.Context, action Action, resource Resource, category Category, resourceID string, kvPairs ...any) {
	if e.actions != nil && !e.actions[action] {
		return
	}

	event := AuditEvent{
		Timestamp:  time.Now(),
		Action:     action,
		Resource:   resource,
		Category:   category,
		ResourceID: resourceID,
	}

	if len(kvPairs) > 0 {
		event.Details = make(map[string]any, len(kvPairs)/2)
		for i := 0; i+1 < len(kvPairs); i += 2 {
			if k, ok := kvPairs[i].(string); ok {
				event.Details[k] = kvPairs[i+1]
			}
		}
	}

	if err := e.recorder.Record(ctx, event); err != nil {
		e.logger.Warn("audit_hook: failed to record event",
			"action", action,
			"error", err,
		)
	}
}

// ──────────────────────────────────────────────────
// Request lifecycle hooks
// ──────────────────────────────────────────────────

func (e *Extension) OnRequestReceived(ctx context.Context, requestID id.RequestID, model, tenantID string) error {
	e.record(ctx, ActionRequestReceived, ResourceRequest, CategoryRequest, requestID.String(),
		"model", model, "tenant_id", tenantID)
	return nil
}

func (e *Extension) OnRequestCompleted(ctx context.Context, requestID id.RequestID, model, providerName string, elapsed time.Duration, inputTokens, outputTokens int) error {
	e.record(ctx, ActionRequestCompleted, ResourceRequest, CategoryRequest, requestID.String(),
		"model", model, "provider", providerName, "elapsed_ms", elapsed.Milliseconds(),
		"input_tokens", inputTokens, "output_tokens", outputTokens)
	return nil
}

func (e *Extension) OnRequestFailed(ctx context.Context, requestID id.RequestID, model string, err error) error {
	e.record(ctx, ActionRequestFailed, ResourceRequest, CategoryRequest, requestID.String(),
		"model", model, "error", err.Error())
	return nil
}

func (e *Extension) OnRequestCached(ctx context.Context, requestID id.RequestID, model string) error {
	e.record(ctx, ActionRequestCached, ResourceRequest, CategoryRequest, requestID.String(),
		"model", model)
	return nil
}

// ──────────────────────────────────────────────────
// Provider lifecycle hooks
// ──────────────────────────────────────────────────

func (e *Extension) OnProviderFailed(ctx context.Context, providerName, model string, err error) error {
	e.record(ctx, ActionProviderFailed, ResourceProvider, CategoryProvider, providerName,
		"model", model, "error", err.Error())
	return nil
}

func (e *Extension) OnCircuitOpened(ctx context.Context, providerName string) error {
	e.record(ctx, ActionCircuitOpened, ResourceProvider, CategoryProvider, providerName)
	return nil
}

func (e *Extension) OnFallbackTriggered(ctx context.Context, from, to string) error {
	e.record(ctx, ActionFallbackTriggered, ResourceProvider, CategoryProvider, from,
		"to", to)
	return nil
}

// ──────────────────────────────────────────────────
// Guardrail lifecycle hooks
// ──────────────────────────────────────────────────

func (e *Extension) OnGuardrailBlocked(ctx context.Context, guardName string, requestID id.RequestID) error {
	e.record(ctx, ActionGuardrailBlocked, ResourceGuardrail, CategorySecurity, guardName,
		"request_id", requestID.String())
	return nil
}

func (e *Extension) OnGuardrailRedacted(ctx context.Context, guardName, field string) error {
	e.record(ctx, ActionGuardrailRedacted, ResourceGuardrail, CategorySecurity, guardName,
		"field", field)
	return nil
}

// ──────────────────────────────────────────────────
// Tenant & key lifecycle hooks
// ──────────────────────────────────────────────────

func (e *Extension) OnTenantCreated(ctx context.Context, tenantID id.TenantID) error {
	e.record(ctx, ActionTenantCreated, ResourceTenant, CategoryTenant, tenantID.String())
	return nil
}

func (e *Extension) OnTenantDisabled(ctx context.Context, tenantID id.TenantID) error {
	e.record(ctx, ActionTenantDisabled, ResourceTenant, CategoryTenant, tenantID.String())
	return nil
}

func (e *Extension) OnKeyCreated(ctx context.Context, keyID id.KeyID, tenantID id.TenantID) error {
	e.record(ctx, ActionKeyCreated, ResourceKey, CategoryTenant, keyID.String(),
		"tenant_id", tenantID.String())
	return nil
}

func (e *Extension) OnKeyRevoked(ctx context.Context, keyID id.KeyID) error {
	e.record(ctx, ActionKeyRevoked, ResourceKey, CategoryTenant, keyID.String())
	return nil
}

// ──────────────────────────────────────────────────
// Budget lifecycle hooks
// ──────────────────────────────────────────────────

func (e *Extension) OnBudgetWarning(ctx context.Context, tenantID id.TenantID, usedPct float64) error {
	e.record(ctx, ActionBudgetWarning, ResourceTenant, CategoryBudget, tenantID.String(),
		"used_pct", usedPct)
	return nil
}

func (e *Extension) OnBudgetExceeded(ctx context.Context, tenantID id.TenantID) error {
	e.record(ctx, ActionBudgetExceeded, ResourceTenant, CategoryBudget, tenantID.String())
	return nil
}

// Compile-time interface checks.
var (
	_ plugin.Extension         = (*Extension)(nil)
	_ plugin.RequestReceived   = (*Extension)(nil)
	_ plugin.RequestCompleted  = (*Extension)(nil)
	_ plugin.RequestFailed     = (*Extension)(nil)
	_ plugin.RequestCached     = (*Extension)(nil)
	_ plugin.ProviderFailed    = (*Extension)(nil)
	_ plugin.CircuitOpened     = (*Extension)(nil)
	_ plugin.FallbackTriggered = (*Extension)(nil)
	_ plugin.GuardrailBlocked  = (*Extension)(nil)
	_ plugin.GuardrailRedacted = (*Extension)(nil)
	_ plugin.TenantCreated     = (*Extension)(nil)
	_ plugin.TenantDisabled    = (*Extension)(nil)
	_ plugin.KeyCreated        = (*Extension)(nil)
	_ plugin.KeyRevoked        = (*Extension)(nil)
	_ plugin.BudgetWarning     = (*Extension)(nil)
	_ plugin.BudgetExceeded    = (*Extension)(nil)
)

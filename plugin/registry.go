package plugin

import (
	"context"
	"log/slog"
	"time"

	"github.com/xraph/nexus/id"
)

// Named entry types pair a hook implementation with the extension name
// captured at registration time. This avoids type-asserting back to
// Extension inside the emit methods.

type requestReceivedEntry struct {
	name string
	hook RequestReceived
}

type requestCompletedEntry struct {
	name string
	hook RequestCompleted
}

type requestFailedEntry struct {
	name string
	hook RequestFailed
}

type requestCachedEntry struct {
	name string
	hook RequestCached
}

type providerFailedEntry struct {
	name string
	hook ProviderFailed
}

type circuitOpenedEntry struct {
	name string
	hook CircuitOpened
}

type fallbackTriggeredEntry struct {
	name string
	hook FallbackTriggered
}

type guardrailBlockedEntry struct {
	name string
	hook GuardrailBlocked
}

type guardrailRedactedEntry struct {
	name string
	hook GuardrailRedacted
}

type tenantCreatedEntry struct {
	name string
	hook TenantCreated
}

type tenantDisabledEntry struct {
	name string
	hook TenantDisabled
}

type keyCreatedEntry struct {
	name string
	hook KeyCreated
}

type keyRevokedEntry struct {
	name string
	hook KeyRevoked
}

type budgetWarningEntry struct {
	name string
	hook BudgetWarning
}

type budgetExceededEntry struct {
	name string
	hook BudgetExceeded
}

// Registry holds registered extensions and dispatches lifecycle events
// to them. It type-caches extensions at registration time so emit calls
// iterate only over extensions that implement the relevant hook.
type Registry struct {
	extensions []Extension
	logger     *slog.Logger

	// Type-cached slices for each lifecycle hook.
	requestReceived   []requestReceivedEntry
	requestCompleted  []requestCompletedEntry
	requestFailed     []requestFailedEntry
	requestCached     []requestCachedEntry
	providerFailed    []providerFailedEntry
	circuitOpened     []circuitOpenedEntry
	fallbackTriggered []fallbackTriggeredEntry
	guardrailBlocked  []guardrailBlockedEntry
	guardrailRedacted []guardrailRedactedEntry
	tenantCreated     []tenantCreatedEntry
	tenantDisabled    []tenantDisabledEntry
	keyCreated        []keyCreatedEntry
	keyRevoked        []keyRevokedEntry
	budgetWarning     []budgetWarningEntry
	budgetExceeded    []budgetExceededEntry
}

// NewRegistry creates an extension registry with a default logger.
func NewRegistry() *Registry {
	return &Registry{logger: slog.Default()}
}

// NewRegistryWithLogger creates an extension registry with the given logger.
func NewRegistryWithLogger(logger *slog.Logger) *Registry {
	return &Registry{logger: logger}
}

// Register adds an extension and type-asserts it into all applicable
// hook caches. Extensions are notified in registration order.
func (r *Registry) Register(e Extension) {
	r.extensions = append(r.extensions, e)
	name := e.Name()

	if h, ok := e.(RequestReceived); ok {
		r.requestReceived = append(r.requestReceived, requestReceivedEntry{name, h})
	}
	if h, ok := e.(RequestCompleted); ok {
		r.requestCompleted = append(r.requestCompleted, requestCompletedEntry{name, h})
	}
	if h, ok := e.(RequestFailed); ok {
		r.requestFailed = append(r.requestFailed, requestFailedEntry{name, h})
	}
	if h, ok := e.(RequestCached); ok {
		r.requestCached = append(r.requestCached, requestCachedEntry{name, h})
	}
	if h, ok := e.(ProviderFailed); ok {
		r.providerFailed = append(r.providerFailed, providerFailedEntry{name, h})
	}
	if h, ok := e.(CircuitOpened); ok {
		r.circuitOpened = append(r.circuitOpened, circuitOpenedEntry{name, h})
	}
	if h, ok := e.(FallbackTriggered); ok {
		r.fallbackTriggered = append(r.fallbackTriggered, fallbackTriggeredEntry{name, h})
	}
	if h, ok := e.(GuardrailBlocked); ok {
		r.guardrailBlocked = append(r.guardrailBlocked, guardrailBlockedEntry{name, h})
	}
	if h, ok := e.(GuardrailRedacted); ok {
		r.guardrailRedacted = append(r.guardrailRedacted, guardrailRedactedEntry{name, h})
	}
	if h, ok := e.(TenantCreated); ok {
		r.tenantCreated = append(r.tenantCreated, tenantCreatedEntry{name, h})
	}
	if h, ok := e.(TenantDisabled); ok {
		r.tenantDisabled = append(r.tenantDisabled, tenantDisabledEntry{name, h})
	}
	if h, ok := e.(KeyCreated); ok {
		r.keyCreated = append(r.keyCreated, keyCreatedEntry{name, h})
	}
	if h, ok := e.(KeyRevoked); ok {
		r.keyRevoked = append(r.keyRevoked, keyRevokedEntry{name, h})
	}
	if h, ok := e.(BudgetWarning); ok {
		r.budgetWarning = append(r.budgetWarning, budgetWarningEntry{name, h})
	}
	if h, ok := e.(BudgetExceeded); ok {
		r.budgetExceeded = append(r.budgetExceeded, budgetExceededEntry{name, h})
	}
}

// Extensions returns all registered extensions.
func (r *Registry) Extensions() []Extension { return r.extensions }

// Count returns the number of registered extensions.
func (r *Registry) Count() int { return len(r.extensions) }

// ──────────────────────────────────────────────────
// Request event emitters
// ──────────────────────────────────────────────────

// EmitRequestReceived notifies all extensions that implement RequestReceived.
func (r *Registry) EmitRequestReceived(ctx context.Context, requestID id.RequestID, model string, tenantID string) {
	for _, e := range r.requestReceived {
		if err := e.hook.OnRequestReceived(ctx, requestID, model, tenantID); err != nil {
			r.logHookError("OnRequestReceived", e.name, err)
		}
	}
}

// EmitRequestCompleted notifies all extensions that implement RequestCompleted.
func (r *Registry) EmitRequestCompleted(ctx context.Context, requestID id.RequestID, model string, providerName string, elapsed time.Duration, inputTokens int, outputTokens int) {
	for _, e := range r.requestCompleted {
		if err := e.hook.OnRequestCompleted(ctx, requestID, model, providerName, elapsed, inputTokens, outputTokens); err != nil {
			r.logHookError("OnRequestCompleted", e.name, err)
		}
	}
}

// EmitRequestFailed notifies all extensions that implement RequestFailed.
func (r *Registry) EmitRequestFailed(ctx context.Context, requestID id.RequestID, model string, reqErr error) {
	for _, e := range r.requestFailed {
		if err := e.hook.OnRequestFailed(ctx, requestID, model, reqErr); err != nil {
			r.logHookError("OnRequestFailed", e.name, err)
		}
	}
}

// EmitRequestCached notifies all extensions that implement RequestCached.
func (r *Registry) EmitRequestCached(ctx context.Context, requestID id.RequestID, model string) {
	for _, e := range r.requestCached {
		if err := e.hook.OnRequestCached(ctx, requestID, model); err != nil {
			r.logHookError("OnRequestCached", e.name, err)
		}
	}
}

// ──────────────────────────────────────────────────
// Provider event emitters
// ──────────────────────────────────────────────────

// EmitProviderFailed notifies all extensions that implement ProviderFailed.
func (r *Registry) EmitProviderFailed(ctx context.Context, providerName string, model string, provErr error) {
	for _, e := range r.providerFailed {
		if err := e.hook.OnProviderFailed(ctx, providerName, model, provErr); err != nil {
			r.logHookError("OnProviderFailed", e.name, err)
		}
	}
}

// EmitCircuitOpened notifies all extensions that implement CircuitOpened.
func (r *Registry) EmitCircuitOpened(ctx context.Context, providerName string) {
	for _, e := range r.circuitOpened {
		if err := e.hook.OnCircuitOpened(ctx, providerName); err != nil {
			r.logHookError("OnCircuitOpened", e.name, err)
		}
	}
}

// EmitFallbackTriggered notifies all extensions that implement FallbackTriggered.
func (r *Registry) EmitFallbackTriggered(ctx context.Context, from string, to string) {
	for _, e := range r.fallbackTriggered {
		if err := e.hook.OnFallbackTriggered(ctx, from, to); err != nil {
			r.logHookError("OnFallbackTriggered", e.name, err)
		}
	}
}

// ──────────────────────────────────────────────────
// Guardrail event emitters
// ──────────────────────────────────────────────────

// EmitGuardrailBlocked notifies all extensions that implement GuardrailBlocked.
func (r *Registry) EmitGuardrailBlocked(ctx context.Context, guardName string, requestID id.RequestID) {
	for _, e := range r.guardrailBlocked {
		if err := e.hook.OnGuardrailBlocked(ctx, guardName, requestID); err != nil {
			r.logHookError("OnGuardrailBlocked", e.name, err)
		}
	}
}

// EmitGuardrailRedacted notifies all extensions that implement GuardrailRedacted.
func (r *Registry) EmitGuardrailRedacted(ctx context.Context, guardName string, field string) {
	for _, e := range r.guardrailRedacted {
		if err := e.hook.OnGuardrailRedacted(ctx, guardName, field); err != nil {
			r.logHookError("OnGuardrailRedacted", e.name, err)
		}
	}
}

// ──────────────────────────────────────────────────
// Tenant & key event emitters
// ──────────────────────────────────────────────────

// EmitTenantCreated notifies all extensions that implement TenantCreated.
func (r *Registry) EmitTenantCreated(ctx context.Context, tenantID id.TenantID) {
	for _, e := range r.tenantCreated {
		if err := e.hook.OnTenantCreated(ctx, tenantID); err != nil {
			r.logHookError("OnTenantCreated", e.name, err)
		}
	}
}

// EmitTenantDisabled notifies all extensions that implement TenantDisabled.
func (r *Registry) EmitTenantDisabled(ctx context.Context, tenantID id.TenantID) {
	for _, e := range r.tenantDisabled {
		if err := e.hook.OnTenantDisabled(ctx, tenantID); err != nil {
			r.logHookError("OnTenantDisabled", e.name, err)
		}
	}
}

// EmitKeyCreated notifies all extensions that implement KeyCreated.
func (r *Registry) EmitKeyCreated(ctx context.Context, keyID id.KeyID, tenantID id.TenantID) {
	for _, e := range r.keyCreated {
		if err := e.hook.OnKeyCreated(ctx, keyID, tenantID); err != nil {
			r.logHookError("OnKeyCreated", e.name, err)
		}
	}
}

// EmitKeyRevoked notifies all extensions that implement KeyRevoked.
func (r *Registry) EmitKeyRevoked(ctx context.Context, keyID id.KeyID) {
	for _, e := range r.keyRevoked {
		if err := e.hook.OnKeyRevoked(ctx, keyID); err != nil {
			r.logHookError("OnKeyRevoked", e.name, err)
		}
	}
}

// ──────────────────────────────────────────────────
// Budget event emitters
// ──────────────────────────────────────────────────

// EmitBudgetWarning notifies all extensions that implement BudgetWarning.
func (r *Registry) EmitBudgetWarning(ctx context.Context, tenantID id.TenantID, usedPct float64) {
	for _, e := range r.budgetWarning {
		if err := e.hook.OnBudgetWarning(ctx, tenantID, usedPct); err != nil {
			r.logHookError("OnBudgetWarning", e.name, err)
		}
	}
}

// EmitBudgetExceeded notifies all extensions that implement BudgetExceeded.
func (r *Registry) EmitBudgetExceeded(ctx context.Context, tenantID id.TenantID) {
	for _, e := range r.budgetExceeded {
		if err := e.hook.OnBudgetExceeded(ctx, tenantID); err != nil {
			r.logHookError("OnBudgetExceeded", e.name, err)
		}
	}
}

// logHookError logs a warning when a lifecycle hook returns an error.
// Errors from hooks are never propagated — they must not block the pipeline.
func (r *Registry) logHookError(hook, extName string, err error) {
	r.logger.Warn("extension hook error",
		slog.String("hook", hook),
		slog.String("extension", extName),
		slog.String("error", err.Error()),
	)
}

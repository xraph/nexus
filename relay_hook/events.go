package relay_hook

import "time"

// EventType identifies the kind of lifecycle event.
type EventType string

const (
	// Request events
	EventRequestReceived  EventType = "request.received"
	EventRequestCompleted EventType = "request.completed"
	EventRequestFailed    EventType = "request.failed"
	EventRequestCached    EventType = "request.cached"

	// Provider events
	EventProviderFailed    EventType = "provider.failed"
	EventCircuitOpened     EventType = "circuit.opened"
	EventFallbackTriggered EventType = "fallback.triggered"

	// Guardrail events
	EventGuardrailBlocked  EventType = "guardrail.blocked"
	EventGuardrailRedacted EventType = "guardrail.redacted"

	// Tenant events
	EventTenantCreated  EventType = "tenant.created"
	EventTenantDisabled EventType = "tenant.disabled"

	// Key events
	EventKeyCreated EventType = "key.created"
	EventKeyRevoked EventType = "key.revoked"

	// Budget events
	EventBudgetWarning  EventType = "budget.warning"
	EventBudgetExceeded EventType = "budget.exceeded"
)

// Event is a relay webhook event payload.
type Event struct {
	Type      EventType      `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data"`
}

package audit_hook

// Action is the type of audit event.
type Action string

const (
	// Request actions
	ActionRequestReceived  Action = "request.received"
	ActionRequestCompleted Action = "request.completed"
	ActionRequestFailed    Action = "request.failed"
	ActionRequestCached    Action = "request.cached"

	// Provider actions
	ActionProviderFailed    Action = "provider.failed"
	ActionCircuitOpened     Action = "circuit.opened"
	ActionFallbackTriggered Action = "fallback.triggered"

	// Guardrail actions
	ActionGuardrailBlocked  Action = "guardrail.blocked"
	ActionGuardrailRedacted Action = "guardrail.redacted"

	// Tenant/Key actions
	ActionTenantCreated  Action = "tenant.created"
	ActionTenantDisabled Action = "tenant.disabled"
	ActionKeyCreated     Action = "key.created"
	ActionKeyRevoked     Action = "key.revoked"

	// Budget actions
	ActionBudgetWarning  Action = "budget.warning"
	ActionBudgetExceeded Action = "budget.exceeded"
)

// Resource is the type of resource being audited.
type Resource string

const (
	ResourceRequest   Resource = "request"
	ResourceProvider  Resource = "provider"
	ResourceGuardrail Resource = "guardrail"
	ResourceTenant    Resource = "tenant"
	ResourceKey       Resource = "key"
)

// Category groups audit events for filtering.
type Category string

const (
	CategoryRequest  Category = "request"
	CategoryProvider Category = "provider"
	CategorySecurity Category = "security"
	CategoryTenant   Category = "tenant"
	CategoryBudget   Category = "budget"
)

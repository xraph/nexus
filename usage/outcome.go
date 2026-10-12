package usage

// Outcome says what happened to a request.
type Outcome string

const (
	// OutcomeOK: a provider answered.
	OutcomeOK Outcome = "ok"
	// OutcomeCached: served from the response cache; no provider was called.
	OutcomeCached Outcome = "cached"
	// OutcomeBlocked: a guard refused the request; BlockedBy names the guard.
	OutcomeBlocked Outcome = "blocked"
	// OutcomeRefused: authentication or a quota refused the request;
	// RefusalCode says which.
	OutcomeRefused Outcome = "refused"
	// OutcomeError: the provider call failed.
	OutcomeError Outcome = "error"
)

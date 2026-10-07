package pipeline

import (
	"errors"
	"time"
)

// Refusal is an error a stage returns when it refuses a request before any
// provider is called: authentication, scopes, quotas, budgets. The usage
// stage records it as refused, at exactly $0, under RefusalCode.
type Refusal interface {
	error
	RefusalCode() string
	StatusCode() int
}

// Refusal codes. Each maps to one HTTP status at the edge.
const (
	CodeUnauthenticated = "unauthenticated" // 401
	CodeForbidden       = "forbidden"       // 403
	CodeInvalidRequest  = "invalid_request" // 400
	CodeRateLimited     = "rate_limited"    // 429
	CodeQuotaExceeded   = "quota_exceeded"  // 429
	CodeBudgetExceeded  = "budget_exceeded" // 429
	CodeUnavailable     = "unavailable"     // 503
	CodeContentBlocked  = "content_blocked" // 400, a guard block
)

// RefusalError is the refusal every Nexus stage returns. Limit names the
// limit that refused the request ("60" requests a minute, "10.00" USD), when
// there is one. RetryAfter is how long the caller should wait before the
// limit can let it through, zero when waiting will not help. Unattributed
// asks the usage stage to record the request under no tenant and no key:
// for identities that disagree, and for a tenant that does not exist.
type RefusalError struct {
	Code         string
	Status       int
	Message      string
	Limit        string
	RetryAfter   time.Duration
	Unattributed bool
	Cause        error
}

func (e *RefusalError) Error() string {
	if e.Message != "" {
		return "nexus: " + e.Message
	}
	return "nexus: " + e.Code
}

func (e *RefusalError) Unwrap() error       { return e.Cause }
func (e *RefusalError) RefusalCode() string { return e.Code }
func (e *RefusalError) StatusCode() int     { return e.Status }

// HTTPStatus maps err to the status and code an HTTP edge should answer
// with: a refusal's own, or 500 internal_error for anything else.
func HTTPStatus(err error) (status int, code string) {
	var r Refusal
	if errors.As(err, &r) {
		return r.StatusCode(), r.RefusalCode()
	}
	return 500, "internal_error"
}

// RetryAfter is how long the refusal in err says to wait, or 0.
func RetryAfter(err error) time.Duration {
	var r *RefusalError
	if errors.As(err, &r) {
		return r.RetryAfter
	}
	return 0
}

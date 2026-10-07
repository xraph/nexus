package pipeline

// Refusal is an error a stage returns when it refuses a request before any
// provider is called: authentication, scopes, quotas, budgets. The usage
// stage records it as refused, at exactly $0, under RefusalCode.
type Refusal interface {
	error
	RefusalCode() string
	StatusCode() int
}

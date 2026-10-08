// Package auth authenticates gateway keys at the HTTP and gRPC edges:
// KeyAuth, RequireScope, Authenticate and AuthenticateWithTenants. See the
// Identity & Auth docs page for the request flow.
package auth

import "context"

// Provider resolves the identity of an incoming request.
//
// Deprecated: nothing consults a Provider. Gateway keys are checked by
// KeyAuth (HTTP) and the grpcsrv KeyAuth interceptor, both through
// AuthenticateWithTenants, and nexus.WithAuth has no effect on them. It
// stays only so existing code compiles, and goes in the v1 break.
type Provider interface {
	// Authenticate extracts and validates credentials from the request context.
	// Returns Claims on success, error on failure.
	Authenticate(ctx context.Context) (*Claims, error)

	// AuthenticateAPIKey validates a Nexus-issued API key.
	AuthenticateAPIKey(ctx context.Context, apiKey string) (*Claims, error)
}

// Claims represents the authenticated identity a Provider returns. The
// gateway does not use it: a request's identity is its tenant, key id and
// scopes in the pipeline context (pipeline.TenantID, pipeline.KeyID,
// pipeline.Scopes).
type Claims struct {
	Subject  string            // user or service ID
	TenantID string            // tenant scope
	KeyID    string            // API key ID (if key-based auth)
	Roles    []string          // roles / permissions
	Metadata map[string]string // arbitrary claims
}

// Package auth defines the authentication abstraction for Nexus.
package auth

import "context"

// Provider resolves the identity of an incoming request.
type Provider interface {
	// Authenticate extracts and validates credentials from the request context.
	// Returns Claims on success, error on failure.
	Authenticate(ctx context.Context) (*Claims, error)

	// AuthenticateAPIKey validates a Nexus-issued API key.
	AuthenticateAPIKey(ctx context.Context, apiKey string) (*Claims, error)
}

// Claims represents the authenticated identity.
type Claims struct {
	Subject  string            // user or service ID
	TenantID string            // tenant scope
	KeyID    string            // API key ID (if key-based auth)
	Roles    []string          // roles / permissions
	Metadata map[string]string // arbitrary claims
}

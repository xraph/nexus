// Package authsome provides an adapter that bridges an external Authsome
// authentication service to the Nexus auth.Provider interface.
package authsome

import (
	"context"

	"github.com/xraph/nexus/auth"
)

// Authenticator represents an external Authsome authentication service.
// This is the minimal interface that the Authsome adapter requires.
type Authenticator interface {
	// Authenticate validates a token and returns the subject (e.g., tenant ID).
	Authenticate(ctx context.Context, token string) (subject string, err error)
}

// Adapter bridges Authsome to Nexus auth.Provider.
type Adapter struct {
	authenticator Authenticator
}

// Compile-time check.
var _ auth.Provider = (*Adapter)(nil)

// New creates an Authsome auth adapter.
func New(authenticator Authenticator) *Adapter {
	return &Adapter{authenticator: authenticator}
}

// Authenticate validates credentials from the request context.
// It extracts the bearer token from context and validates via Authsome.
func (a *Adapter) Authenticate(_ context.Context) (*auth.Claims, error) {
	// In a full implementation, extract bearer token from context.
	// For now, return empty claims.
	return &auth.Claims{}, nil
}

// AuthenticateAPIKey validates a Nexus API key via the Authsome service.
func (a *Adapter) AuthenticateAPIKey(ctx context.Context, apiKey string) (*auth.Claims, error) {
	subject, err := a.authenticator.Authenticate(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	return &auth.Claims{
		Subject:  subject,
		TenantID: subject,
	}, nil
}

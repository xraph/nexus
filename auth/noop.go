package auth

import "context"

// NoopProvider allows all requests (for development / single-tenant).
//
// Deprecated: nothing consults an auth.Provider, so this never allowed or
// refused anything. For local work without keys, use
// nexus.WithRequireAPIKey(false), which opens the /v1 routes only.
type NoopProvider struct {
	defaultTenant string
}

// NewNoop creates a noop auth provider that allows all requests.
//
// Deprecated: see NoopProvider.
func NewNoop(defaultTenant ...string) *NoopProvider {
	t := "default"
	if len(defaultTenant) > 0 {
		t = defaultTenant[0]
	}
	return &NoopProvider{defaultTenant: t}
}

func (n *NoopProvider) Authenticate(_ context.Context) (*Claims, error) {
	return &Claims{
		Subject:  "anonymous",
		TenantID: n.defaultTenant,
		Roles:    []string{"admin"},
	}, nil
}

func (n *NoopProvider) AuthenticateAPIKey(ctx context.Context, _ string) (*Claims, error) {
	return n.Authenticate(ctx)
}

package auth

import "context"

// NoopProvider allows all requests (for development / single-tenant).
type NoopProvider struct {
	defaultTenant string
}

// NewNoop creates a noop auth provider that allows all requests.
func NewNoop(defaultTenant ...string) *NoopProvider {
	t := "default"
	if len(defaultTenant) > 0 {
		t = defaultTenant[0]
	}
	return &NoopProvider{defaultTenant: t}
}

func (n *NoopProvider) Authenticate(ctx context.Context) (*Claims, error) {
	return &Claims{
		Subject:  "anonymous",
		TenantID: n.defaultTenant,
		Roles:    []string{"admin"},
	}, nil
}

func (n *NoopProvider) AuthenticateAPIKey(ctx context.Context, key string) (*Claims, error) {
	return n.Authenticate(ctx)
}

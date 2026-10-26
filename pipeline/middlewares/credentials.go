package middlewares

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

// ErrCredentialUnavailable refuses a request whose credential could not be
// resolved, or that resolved to a credential the chosen provider cannot use.
// The request is not sent, and not served on the provider's own key either.
// It is wrapped with the cause; match it with errors.Is.
var ErrCredentialUnavailable error = &pipeline.RefusalError{Code: pipeline.CodeUnavailable, Status: 503, Message: "provider credentials unavailable"}

// WithCredentialProvider resolves a credential before each provider call,
// once per attempt. A cp that implements provider.CredentialResolver is
// asked through it; any other is asked GetCredential, and the key it gives
// is the credential. A nil cp resolves nothing.
//
// Not found (provider.ErrCredentialNotFound, or an empty credential) serves
// the request with the provider's own key. Any other error, or a credential
// for a provider that is not provider.CredentialAware, refuses the request
// with ErrCredentialUnavailable before the provider is called.
func (m *ProviderCallMiddleware) WithCredentialProvider(cp provider.CredentialProvider) *ProviderCallMiddleware {
	switch r := cp.(type) {
	case nil:
		m.credentials = nil
	case provider.CredentialResolver:
		m.credentials = r
	default:
		m.credentials = keyOnly{cp}
	}
	return m
}

// keyOnly adapts a CredentialProvider that only knows keys.
type keyOnly struct{ cp provider.CredentialProvider }

func (k keyOnly) ResolveCredential(ctx context.Context, providerName string) (provider.Credential, error) {
	key, err := k.cp.GetCredential(ctx, providerName)
	if err != nil {
		return provider.Credential{}, err
	}
	return provider.Credential{APIKey: key}, nil
}

// withCredential returns ctx carrying the credential for a call to p, or
// the refusal. The context always ends up with exactly the credential the
// gateway resolved, none when it resolved none: a credential the caller
// put on the context never reaches a provider.
func (m *ProviderCallMiddleware) withCredential(ctx context.Context, p provider.Provider) (context.Context, error) {
	var cred provider.Credential
	if m.credentials != nil {
		resolved, err := m.credentials.ResolveCredential(ctx, p.Name())
		switch {
		case errors.Is(err, provider.ErrCredentialNotFound):
			// None for this tenant: the provider serves with its own key.
		case err != nil:
			// The resolver's error is kept for errors.Is and the logs; the
			// credential it may have half returned is dropped unread.
			return ctx, fmt.Errorf("%w: %w", ErrCredentialUnavailable, err)
		default:
			cred = resolved
		}
	}
	if !cred.IsZero() {
		if aware, ok := p.(provider.CredentialAware); !ok || !aware.AcceptsCredentials() {
			return ctx, fmt.Errorf("%w: provider %s cannot use tenant credentials", ErrCredentialUnavailable, p.Name())
		}
	}
	return provider.WithCredential(ctx, cred), nil
}

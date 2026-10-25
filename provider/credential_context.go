package provider

import (
	"context"
	"errors"
)

// ErrMissingBaseURL is returned, before any network call, by a provider
// asked to send a request when neither its configuration nor the request's
// credential gives it a base URL.
var ErrMissingBaseURL = errors.New("nexus: provider base URL is required")

// Credential is what one request authenticates with: the API key and,
// optionally, the endpoint. The gateway resolves one per provider call
// (see CredentialResolver) and hands it to the provider on the context.
type Credential struct {
	// APIKey replaces the provider's configured key for the request. It is
	// a secret: nothing logs it or puts it in an error, and Credential's
	// String and GoString leave it out.
	APIKey string
	// BaseURL replaces the provider's configured base URL for the request.
	// "" keeps the configured one.
	BaseURL string
}

// IsZero reports whether c carries neither a key nor a base URL, which
// counts as no credential at all.
func (c Credential) IsZero() bool { return c.APIKey == "" && c.BaseURL == "" }

// String describes c without its key.
func (c Credential) String() string {
	key := "none"
	if c.APIKey != "" {
		key = "set"
	}
	return "Credential{APIKey: " + key + ", BaseURL: " + c.BaseURL + "}"
}

// GoString is String, so %#v leaves the key out too.
func (c Credential) GoString() string { return c.String() }

// CredentialResolver is a CredentialProvider that can also choose the
// endpoint: a host that keeps each workspace's own key, and for an
// OpenAI-compatible server its URL, implements it. The gateway calls it
// after routing, once per provider attempt, with pipeline.TenantID(ctx) set
// to the request's nexus tenant ("" for an unattributed request).
//
// Return ErrCredentialNotFound (wrapped or not) to serve the request with
// the provider's own key. Any other error refuses the request: nothing is
// sent and the gateway does not fall back to the provider's key. The error
// must not contain a key.
type CredentialResolver interface {
	ResolveCredential(ctx context.Context, providerName string) (Credential, error)
}

// CredentialAware is implemented by providers that apply a per-request
// Credential from the context: anthropic, openai and opencompat. The
// gateway refuses to send a tenant credential to a provider that does not
// answer true, rather than serve the tenant on the operator's key.
type CredentialAware interface {
	AcceptsCredentials() bool
}

type credentialKey struct{}

// WithCredential returns a context that carries c for the provider call.
// An empty c clears any credential the parent context carried.
func WithCredential(ctx context.Context, c Credential) context.Context {
	return context.WithValue(ctx, credentialKey{}, c)
}

// CredentialFromContext returns the credential ctx carries. ok is false
// when there is none or it is empty.
func CredentialFromContext(ctx context.Context) (Credential, bool) {
	c, ok := ctx.Value(credentialKey{}).(Credential)
	if !ok {
		return Credential{}, false
	}
	return c, !c.IsZero()
}

package middlewares_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
	"github.com/xraph/nexus/router/strategies"
)

// keyed is a routed provider that accepts tenant credentials (when aware)
// and records the credential each call carried.
type keyed struct {
	*routed
	aware    bool
	failures int // fail this many calls first, with a retryable error

	mu   sync.Mutex
	seen []*provider.Credential // nil when the call carried none
}

func newKeyed(name string, aware bool, models ...string) *keyed {
	return &keyed{routed: newRouted(name, true, models...), aware: aware}
}

func (p *keyed) AcceptsCredentials() bool { return p.aware }

func (p *keyed) see(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := provider.CredentialFromContext(ctx); ok {
		p.seen = append(p.seen, &c)
	} else {
		p.seen = append(p.seen, nil)
	}
	if len(p.seen) <= p.failures {
		return errors.New("upstream 502")
	}
	return nil
}

func (p *keyed) Seen() []*provider.Credential {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*provider.Credential(nil), p.seen...)
}

func (p *keyed) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	if err := p.see(ctx); err != nil {
		return nil, err
	}
	return p.routed.Complete(ctx, req)
}

func (p *keyed) CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	if err := p.see(ctx); err != nil {
		return nil, err
	}
	return p.routed.CompleteStream(ctx, req)
}

func (p *keyed) Embed(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	if err := p.see(ctx); err != nil {
		return nil, err
	}
	return p.routed.Embed(ctx, req)
}

// resolverFunc is a CredentialResolver (and so a CredentialProvider) from a
// function. It counts its calls and keeps what each one was asked.
type resolverFunc struct {
	fn func(ctx context.Context, providerName string) (provider.Credential, error)

	mu      sync.Mutex
	tenants []string
	names   []string
}

func resolver(fn func(ctx context.Context, providerName string) (provider.Credential, error)) *resolverFunc {
	return &resolverFunc{fn: fn}
}

func (r *resolverFunc) ResolveCredential(ctx context.Context, providerName string) (provider.Credential, error) {
	r.mu.Lock()
	r.tenants = append(r.tenants, pipeline.TenantID(ctx))
	r.names = append(r.names, providerName)
	r.mu.Unlock()
	return r.fn(ctx, providerName)
}

func (r *resolverFunc) GetCredential(context.Context, string) (string, error) {
	return "", errors.New("the provider call must use ResolveCredential when it is there")
}

func (r *resolverFunc) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.names)
}

func requestOf(typ pipeline.RequestType, modelID, providerName string) *pipeline.Request {
	if typ == pipeline.RequestEmbedding {
		return routedEmbedding(modelID, providerName)
	}
	req := routedCompletion(modelID, providerName)
	req.Type = typ
	return req
}

var credentialKinds = []pipeline.RequestType{pipeline.RequestCompletion, pipeline.RequestStream, pipeline.RequestEmbedding}

func TestTenantCredentialReachesTheProvider(t *testing.T) {
	for _, typ := range credentialKinds {
		t.Run(string(typ), func(t *testing.T) {
			openai := newKeyed("openai", true, "gpt-4o")
			other := newKeyed("other", true, "other-model")
			reg := provider.NewRegistry()
			reg.Register(other)
			reg.Register(openai)
			want := provider.Credential{APIKey: "sk-tenant", BaseURL: "https://tenant.example.com/v1"}
			res := resolver(func(context.Context, string) (provider.Credential, error) { return want, nil })
			mw := middlewares.NewProviderCall(router.NewService(strategies.NewPriority()), reg).WithCredentialProvider(res)

			ctx := pipeline.WithTenantID(context.Background(), "tenant_T")
			if _, err := mw.Process(ctx, requestOf(typ, "gpt-4o", ""), nil); err != nil {
				t.Fatalf("Process: %v", err)
			}
			seen := openai.Seen()
			if len(seen) != 1 || seen[0] == nil || *seen[0] != want {
				t.Fatalf("provider saw %v, want one call with %+v", seen, want)
			}
			if len(other.Seen()) != 0 {
				t.Fatal("routing picked the wrong provider")
			}
			if res.tenants[0] != "tenant_T" || res.names[0] != "openai" {
				t.Fatalf("resolver asked for tenant %q provider %q, want tenant_T openai", res.tenants[0], res.names[0])
			}
		})
	}
}

func TestNotFoundUsesTheProvidersOwnKey(t *testing.T) {
	notFound := map[string]func(context.Context, string) (provider.Credential, error){
		"sentinel": func(context.Context, string) (provider.Credential, error) {
			return provider.Credential{}, provider.ErrCredentialNotFound
		},
		// Wrapped, and with a stray key the gateway must ignore.
		"wrapped": func(_ context.Context, name string) (provider.Credential, error) {
			return provider.Credential{APIKey: "sk-ignored"}, fmt.Errorf("vault: no key for %s: %w", name, provider.ErrCredentialNotFound)
		},
		"empty": func(context.Context, string) (provider.Credential, error) {
			return provider.Credential{}, nil
		},
	}
	for name, fn := range notFound {
		for _, typ := range credentialKinds {
			t.Run(name+"/"+string(typ), func(t *testing.T) {
				p := newKeyed("openai", true, "gpt-4o")
				reg := provider.NewRegistry()
				reg.Register(p)
				mw := middlewares.NewProviderCall(nil, reg).WithCredentialProvider(resolver(fn))

				if _, err := mw.Process(context.Background(), requestOf(typ, "gpt-4o", "openai"), nil); err != nil {
					t.Fatalf("Process: %v", err)
				}
				if seen := p.Seen(); len(seen) != 1 || seen[0] != nil {
					t.Fatalf("provider saw %v, want one call with no credential", seen)
				}
			})
		}
	}
}

func TestOnlyTheGatewayPutsACredentialOnTheCall(t *testing.T) {
	// A credential already on the caller's context is never passed on: the
	// provider call decides, so a key cannot reach a provider it was not
	// resolved for.
	stale := provider.WithCredential(context.Background(), provider.Credential{APIKey: "sk-stale"})
	for name, cp := range map[string]provider.CredentialProvider{
		"no resolver": nil,
		"not found": resolver(func(context.Context, string) (provider.Credential, error) {
			return provider.Credential{}, provider.ErrCredentialNotFound
		}),
	} {
		t.Run(name, func(t *testing.T) {
			p := newKeyed("openai", true, "gpt-4o")
			reg := provider.NewRegistry()
			reg.Register(p)
			mw := middlewares.NewProviderCall(nil, reg)
			if cp != nil {
				mw = mw.WithCredentialProvider(cp)
			}
			if _, err := mw.Process(stale, requestOf(pipeline.RequestCompletion, "gpt-4o", "openai"), nil); err != nil {
				t.Fatalf("Process: %v", err)
			}
			if seen := p.Seen(); len(seen) != 1 || seen[0] != nil {
				t.Fatalf("provider saw %v, want no credential", seen)
			}
		})
	}
}

func TestCredentialErrorRefusesWithoutFallback(t *testing.T) {
	for _, typ := range credentialKinds {
		t.Run(string(typ), func(t *testing.T) {
			p := newKeyed("openai", true, "gpt-4o")
			reg := provider.NewRegistry()
			reg.Register(p)
			vault := errors.New("vault: timeout")
			// A resolver that fails part way may still hand back a key; it
			// must not reach the provider or the error.
			res := resolver(func(context.Context, string) (provider.Credential, error) {
				return provider.Credential{APIKey: "sk-half-read"}, vault
			})
			mw := middlewares.NewProviderCall(nil, reg).WithCredentialProvider(res)

			attempts, err := callOnceThroughRetry(t, mw, requestOf(typ, "gpt-4o", "openai"))
			if !errors.Is(err, middlewares.ErrCredentialUnavailable) {
				t.Fatalf("err = %v, want ErrCredentialUnavailable", err)
			}
			if !errors.Is(err, vault) {
				t.Fatalf("err = %v, want the resolver's error wrapped", err)
			}
			if status, code := pipeline.HTTPStatus(err); status != 503 || code != pipeline.CodeUnavailable {
				t.Fatalf("status %d code %q, want 503 unavailable", status, code)
			}
			if strings.Contains(err.Error(), "sk-half-read") {
				t.Fatalf("error text carries the key: %v", err)
			}
			if attempts != 1 || res.Calls() != 1 {
				t.Fatalf("attempts %d, resolver calls %d; a refusal is not retried", attempts, res.Calls())
			}
			if len(p.Seen()) != 0 {
				t.Fatal("the provider was called after the resolver failed")
			}
		})
	}
}

func TestTenantCredentialForUnawareProviderRefuses(t *testing.T) {
	for name, aware := range map[string]bool{"no AcceptsCredentials": false, "AcceptsCredentials false": true} {
		t.Run(name, func(t *testing.T) {
			var p provider.Provider
			var calls func() int
			if aware {
				k := newKeyed("groq", false, "llama")
				p, calls = k, func() int { return len(k.Seen()) }
			} else {
				r := newRouted("groq", true, "llama")
				p, calls = r, r.Calls
			}
			reg := provider.NewRegistry()
			reg.Register(p)
			res := resolver(func(context.Context, string) (provider.Credential, error) {
				return provider.Credential{APIKey: "sk-tenant"}, nil
			})
			mw := middlewares.NewProviderCall(nil, reg).WithCredentialProvider(res)

			_, err := mw.Process(context.Background(), requestOf(pipeline.RequestCompletion, "llama", "groq"), nil)
			if !errors.Is(err, middlewares.ErrCredentialUnavailable) {
				t.Fatalf("err = %v, want ErrCredentialUnavailable", err)
			}
			if !strings.Contains(err.Error(), "provider groq cannot use tenant credentials") {
				t.Fatalf("err = %v, want it to name the provider", err)
			}
			if strings.Contains(err.Error(), "sk-tenant") {
				t.Fatalf("error text carries the key: %v", err)
			}
			if calls() != 0 {
				t.Fatal("the provider was called with a credential it cannot use")
			}
		})
	}
}

func TestGetCredentialOnlyResolverIsAdapted(t *testing.T) {
	openai := newKeyed("openai", true, "gpt-4o")
	groq := newRouted("groq", true, "llama")
	reg := provider.NewRegistry()
	reg.Register(openai)
	reg.Register(groq)
	mw := middlewares.NewProviderCall(nil, reg).
		WithCredentialProvider(provider.NewStaticCredential(map[string]string{"openai": "sk-static"}))

	if _, err := mw.Process(context.Background(), requestOf(pipeline.RequestCompletion, "gpt-4o", "openai"), nil); err != nil {
		t.Fatalf("openai: %v", err)
	}
	if seen := openai.Seen(); len(seen) != 1 || seen[0] == nil || *seen[0] != (provider.Credential{APIKey: "sk-static"}) {
		t.Fatalf("openai saw %v, want the static key", seen)
	}

	// No entry for groq: not found, so groq (which cannot take a tenant
	// key) serves with its own.
	if _, err := mw.Process(context.Background(), requestOf(pipeline.RequestCompletion, "llama", "groq"), nil); err != nil {
		t.Fatalf("groq: %v", err)
	}
	if groq.Calls() != 1 {
		t.Fatalf("groq calls = %d, want 1", groq.Calls())
	}
}

func TestCredentialIsResolvedOnEveryAttempt(t *testing.T) {
	p := newKeyed("openai", true, "gpt-4o")
	p.failures = 1
	reg := provider.NewRegistry()
	reg.Register(p)
	res := resolver(func(context.Context, string) (provider.Credential, error) {
		return provider.Credential{APIKey: "sk-tenant"}, nil
	})
	mw := middlewares.NewProviderCall(nil, reg).WithCredentialProvider(res)

	attempts, err := callOnceThroughRetry(t, mw, requestOf(pipeline.RequestCompletion, "gpt-4o", "openai"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if attempts != 2 || res.Calls() != 2 {
		t.Fatalf("attempts %d, resolver calls %d; want 2 and 2", attempts, res.Calls())
	}
	for i, c := range p.Seen() {
		if c == nil || c.APIKey != "sk-tenant" {
			t.Fatalf("attempt %d carried %v, want the tenant key", i, c)
		}
	}
}

// misconfigured is a provider that fails every call with err.
type misconfigured struct {
	*routed
	err error
}

func (p *misconfigured) Complete(_ context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	p.record(req.Model)
	return nil, p.err
}

func (p *misconfigured) CompleteStream(_ context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	p.record(req.Model)
	return nil, p.err
}

func (p *misconfigured) Embed(_ context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	p.record(req.Model)
	return nil, p.err
}

func TestMissingKeyOrBaseURLIsNotRetried(t *testing.T) {
	for _, sentinel := range []error{provider.ErrMissingAPIKey, provider.ErrMissingBaseURL} {
		for _, typ := range credentialKinds {
			t.Run(sentinel.Error()+"/"+string(typ), func(t *testing.T) {
				p := &misconfigured{routed: newRouted("openai", true, "gpt-4o"), err: fmt.Errorf("openai: %w", sentinel)}
				reg := provider.NewRegistry()
				reg.Register(p)
				mw := middlewares.NewProviderCall(nil, reg)

				attempts, err := callOnceThroughRetry(t, mw, requestOf(typ, "gpt-4o", "openai"))
				if !errors.Is(err, sentinel) || !pipeline.IsPermanent(err) {
					t.Fatalf("err = %v, want %v marked permanent", err, sentinel)
				}
				if attempts != 1 || p.Calls() != 1 {
					t.Fatalf("attempts %d, provider calls %d; want 1 and 1", attempts, p.Calls())
				}
			})
		}
	}
}

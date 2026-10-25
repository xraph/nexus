package nexus_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/usage"
)

// keyedProvider stands in for a provider that takes tenant credentials
// (anthropic, openai, opencompat): it serves with the context credential
// when there is one, else with its own key, and records which key each
// call used.
type keyedProvider struct {
	*fakeProvider
	ownKey string

	mu   sync.Mutex
	used []string
}

func (p *keyedProvider) AcceptsCredentials() bool { return true }

func (p *keyedProvider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	key := p.ownKey
	if c, ok := provider.CredentialFromContext(ctx); ok && c.APIKey != "" {
		key = c.APIKey
	}
	p.mu.Lock()
	p.used = append(p.used, key)
	p.mu.Unlock()
	return p.fakeProvider.Complete(ctx, req)
}

func (p *keyedProvider) Used() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.used...)
}

// tenantKeys resolves a credential per nexus tenant: a key for the tenants
// it holds, not found for the rest, and a failure for the ones marked down.
type tenantKeys struct {
	mu   sync.Mutex
	keys map[string]string
	down map[string]bool
}

func (r *tenantKeys) set(tenantID, key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys[tenantID] = key
}

func (r *tenantKeys) fail(tenantID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.down[tenantID] = true
}

func (r *tenantKeys) ResolveCredential(ctx context.Context, providerName string) (provider.Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tenantID := pipeline.TenantID(ctx)
	if r.down[tenantID] {
		return provider.Credential{}, errors.New("vault: timeout")
	}
	if key, ok := r.keys[tenantID]; ok {
		return provider.Credential{APIKey: key}, nil
	}
	return provider.Credential{}, fmt.Errorf("no %s key for %q: %w", providerName, tenantID, provider.ErrCredentialNotFound)
}

func (r *tenantKeys) GetCredential(ctx context.Context, providerName string) (string, error) {
	c, err := r.ResolveCredential(ctx, providerName)
	return c.APIKey, err
}

// capturedLogs is a Logger that keeps every line it is given.
type capturedLogs struct {
	mu    sync.Mutex
	lines []string
}

func (l *capturedLogs) log(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprint(append([]any{msg}, args...)...))
}
func (l *capturedLogs) Debug(msg string, args ...any) { l.log(msg, args...) }
func (l *capturedLogs) Info(msg string, args ...any)  { l.log(msg, args...) }
func (l *capturedLogs) Warn(msg string, args ...any)  { l.log(msg, args...) }
func (l *capturedLogs) Error(msg string, args ...any) { l.log(msg, args...) }

func (l *capturedLogs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func TestOneGatewayServesGlobalAndTenantKeys(t *testing.T) {
	p := &keyedProvider{fakeProvider: &fakeProvider{name: "anthropic", price: listPrice}, ownKey: "global"}
	keys := &tenantKeys{keys: map[string]string{}, down: map[string]bool{}}
	logs := &capturedLogs{}

	var a, b, c string
	var refused error
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		a, _ = tenantAndKey(t, gw)
		b, _ = tenantAndKey(t, gw)
		c, _ = tenantAndKey(t, gw)
		keys.set(a, "key-a")
		keys.fail(c)

		for _, tenantID := range []string{a, b} {
			_, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", TenantID: tenantID,
				Messages: []provider.Message{{Role: "user", Content: "hi"}}})
			if err != nil {
				t.Fatalf("complete for %s: %v", tenantID, err)
			}
		}
		_, refused = gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", TenantID: c,
			Messages: []provider.Message{{Role: "user", Content: "hi"}}})
	}, nexus.WithProvider(p), nexus.WithCredentialProvider(keys), nexus.WithLogger(logs))

	if used := p.Used(); len(used) != 2 || used[0] != "key-a" || used[1] != "global" {
		t.Fatalf("keys used = %v, want [key-a global]: tenant A brings its key, tenant B falls back", used)
	}

	if !errors.Is(refused, nexus.ErrCredentialUnavailable) {
		t.Fatalf("tenant C: err = %v, want ErrCredentialUnavailable", refused)
	}
	if status, code := pipeline.HTTPStatus(refused); status != 503 || code != "unavailable" {
		t.Fatalf("tenant C: status %d code %q, want 503 unavailable", status, code)
	}

	byTenant := map[string]*usage.Record{}
	for _, r := range recs {
		byTenant[r.TenantID.String()] = r
	}
	if len(recs) != 3 || byTenant[a] == nil || byTenant[b] == nil || byTenant[c] == nil {
		t.Fatalf("records = %+v, want one each for A, B and C", recs)
	}
	for _, tenantID := range []string{a, b} {
		if r := byTenant[tenantID]; r.Outcome != usage.OutcomeOK || r.Provider != "anthropic" || r.CostUSD == nil {
			t.Fatalf("record for %s = %+v, want a priced ok call to anthropic", tenantID, r)
		}
	}
	if r := byTenant[c]; r.Outcome != usage.OutcomeRefused || r.RefusalCode != "unavailable" || r.CostUSD == nil || !r.CostUSD.IsZero() {
		t.Fatalf("record for C = %+v, want refused unavailable at $0", r)
	}

	// The tenant key is in no record, no log line and no error.
	raw, err := json.Marshal(recs)
	if err != nil {
		t.Fatalf("marshal records: %v", err)
	}
	for where, text := range map[string]string{"records": string(raw), "logs": logs.String(), "error": refused.Error()} {
		if strings.Contains(text, "key-a") {
			t.Errorf("%s carry the tenant key: %s", where, text)
		}
	}
}

func TestCredentialNotFoundIsOneValue(t *testing.T) {
	if nexus.ErrCredentialNotFound != provider.ErrCredentialNotFound { //nolint:errorlint // identity is the point
		t.Fatal("nexus.ErrCredentialNotFound and provider.ErrCredentialNotFound differ")
	}
}

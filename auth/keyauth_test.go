package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/nexus/auth"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
)

func keys(t *testing.T) (key.Service, string, string, *key.APIKey) {
	t.Helper()
	s := store.NewMemory()
	tn, err := tenant.NewService(s.Tenants()).Create(context.Background(), &tenant.CreateInput{Name: "A", Slug: "a"})
	if err != nil {
		t.Fatal(err)
	}
	ks := key.NewService(s.Keys(), key.WithTenants(s.Tenants()))
	k, raw, err := ks.Create(context.Background(), &key.CreateInput{TenantID: tn.ID.String(), Name: "k", Scopes: []string{"completions"}})
	if err != nil {
		t.Fatal(err)
	}
	_, admin, err := ks.Create(context.Background(), &key.CreateInput{TenantID: tn.ID.String(), Name: "ops", Scopes: []string{"admin"}})
	if err != nil {
		t.Fatal(err)
	}
	return ks, raw, admin, k
}

func serve(h http.Handler, header, value string) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/chat/completions", nil)
	if header != "" {
		r.Header.Set(header, value)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestEitherHeaderAuthenticates(t *testing.T) {
	ks, raw, _, k := keys(t)
	var seen context.Context
	h := auth.KeyAuth(auth.KeyAuthOptions{Keys: ks, Required: true})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = r.Context() }))
	for _, hv := range [][2]string{{"Authorization", "Bearer " + raw}, {"x-api-key", raw}} {
		if w := serve(h, hv[0], hv[1]); w.Code != 200 {
			t.Fatalf("%s: %d %s", hv[0], w.Code, w.Body)
		}
		scopes, ok := pipeline.Scopes(seen)
		if pipeline.KeyID(seen) != k.ID.String() || pipeline.TenantID(seen) != k.TenantID.String() || !ok || len(scopes) != 1 {
			t.Fatalf("%s: context not set", hv[0])
		}
	}
}

func TestMissingWrongAndRevokedKeysAre401(t *testing.T) {
	ks, raw, _, k := keys(t)
	h := auth.KeyAuth(auth.KeyAuthOptions{Keys: ks, Required: true})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if w := serve(h, "", ""); w.Code != 401 {
		t.Fatalf("no key = %d", w.Code)
	}
	if w := serve(h, "Authorization", "Bearer nxs_"+raw[4:10]+"nope"); w.Code != 401 {
		t.Fatalf("wrong key = %d", w.Code)
	}
	if err := ks.Revoke(context.Background(), k.ID.String()); err != nil {
		t.Fatal(err)
	}
	w := serve(h, "x-api-key", raw)
	if w.Code != 401 {
		t.Fatalf("revoked = %d", w.Code)
	}
	if contains(w.Body.String(), raw) || contains(w.Header().Get("WWW-Authenticate"), raw) {
		t.Fatal("response echoed the raw key")
	}
}

func TestAnOpenGatewayStillChecksAKeyThatIsPresented(t *testing.T) {
	ks, _, _, _ := keys(t)
	h := auth.KeyAuth(auth.KeyAuthOptions{Keys: ks, Required: false})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if w := serve(h, "", ""); w.Code != 200 {
		t.Fatalf("open gateway without a key = %d", w.Code)
	}
	if w := serve(h, "x-api-key", "nxs_bogus"); w.Code != 401 {
		t.Fatalf("open gateway with a bad key = %d", w.Code)
	}
}

func TestRequireScope(t *testing.T) {
	ks, raw, admin, _ := keys(t)
	h := auth.KeyAuth(auth.KeyAuthOptions{Keys: ks, Required: true})(auth.RequireScope("admin", nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	if w := serve(h, "x-api-key", raw); w.Code != 403 {
		t.Fatalf("completions key on an admin route = %d", w.Code)
	}
	if w := serve(h, "x-api-key", admin); w.Code != 200 {
		t.Fatalf("admin key = %d", w.Code)
	}
}

func TestRequireScopeLetsAnOpenGatewayThrough(t *testing.T) {
	h := auth.RequireScope("admin", nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if w := serve(h, "", ""); w.Code != 200 {
		t.Fatalf("request with no scopes in context = %d", w.Code)
	}
}

type brokenKeys struct{ err error }

func (b brokenKeys) Validate(context.Context, string) (*key.APIKey, error) { return nil, b.err }

func TestAStoreFailureIs503AndHidesTheCauseAndTheKey(t *testing.T) {
	const raw = "nxs_0123456789abcdef"
	h := auth.KeyAuth(auth.KeyAuthOptions{Keys: brokenKeys{errors.New("db down for " + raw)}, Required: true})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	w := serve(h, "x-api-key", raw)
	if w.Code != 503 {
		t.Fatalf("store failure = %d", w.Code)
	}
	if contains(w.Body.String(), raw) || contains(w.Body.String(), "db down") {
		t.Fatal("response leaked the cause or the raw key")
	}
}

func TestWriteErrorEnvelopeAndRetryAfter(t *testing.T) {
	w := httptest.NewRecorder()
	auth.WriteError(w, &pipeline.RefusalError{Code: pipeline.CodeRateLimited, Status: 429, Message: "slow down", RetryAfter: 1500 * time.Millisecond})
	if w.Code != 429 || w.Header().Get("Retry-After") != "2" {
		t.Fatalf("status %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	body := w.Body.String()
	for _, want := range []string{`"type":"rate_limit_error"`, `"code":"rate_limited"`, `"message":"nexus: slow down"`} {
		if !contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
}

func TestRawKeyIsEmptyWithoutABearerOrApiKeyHeader(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	if got := auth.RawKey(r); got != "" {
		t.Fatalf("RawKey with no headers = %q", got)
	}
	r.Header.Set("Authorization", "Basic abc")
	if got := auth.RawKey(r); got != "" {
		t.Fatalf("RawKey with a Basic header = %q", got)
	}
}

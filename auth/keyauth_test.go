package auth_test

import (
	"context"
	"errors"
	"fmt"
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
	for _, hv := range [][2]string{{"Authorization", "Bearer " + raw}, {"Authorization", "bearer " + raw}, {"x-api-key", raw}} {
		seen = nil
		if w := serve(h, hv[0], hv[1]); w.Code != 200 {
			t.Fatalf("%s: %d %s", hv[0], w.Code, w.Body)
		}
		if seen == nil {
			t.Fatalf("%s: the handler did not run", hv[0])
		}
		scopes, ok := pipeline.Scopes(seen)
		if pipeline.KeyID(seen) != k.ID.String() || pipeline.TenantID(seen) != k.TenantID.String() || !ok || len(scopes) != 1 || scopes[0] != "completions" {
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

func TestRequireScopeFailsClosed(t *testing.T) {
	ks, raw, _, _ := keys(t)
	ok := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	// KeyAuth never ran.
	if w := serve(auth.RequireScope("admin", nil)(ok), "", ""); w.Code != 401 {
		t.Fatalf("RequireScope without KeyAuth = %d", w.Code)
	}
	if w := serve(auth.RequireScope("admin", nil)(ok), "x-api-key", raw); w.Code != 401 {
		t.Fatalf("RequireScope without KeyAuth, key presented = %d", w.Code)
	}
	// KeyAuth let an anonymous request through.
	open := auth.KeyAuth(auth.KeyAuthOptions{Keys: ks, Required: false})(auth.RequireScope("admin", nil)(ok))
	w := serve(open, "", "")
	if w.Code != 401 || w.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("anonymous request behind an open KeyAuth = %d, WWW-Authenticate %q", w.Code, w.Header().Get("WWW-Authenticate"))
	}
	// A key without the scope.
	if w := serve(open, "x-api-key", raw); w.Code != 403 {
		t.Fatalf("key without the scope behind an open KeyAuth = %d", w.Code)
	}
	// Scopes set by something other than KeyAuth do not count.
	r := httptest.NewRequestWithContext(pipeline.WithScopes(context.Background(), []string{"admin"}), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	auth.RequireScope("admin", nil)(ok).ServeHTTP(rec, r)
	if rec.Code != 401 {
		t.Fatalf("scopes without the KeyAuth marker = %d", rec.Code)
	}
}

func TestAPresentedButUnusableAuthorizationHeaderIs401EvenOnAnOpenGateway(t *testing.T) {
	ks, raw, _, _ := keys(t)
	h := auth.KeyAuth(auth.KeyAuthOptions{Keys: ks, Required: false})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for name, v := range map[string]string{"basic": "Basic abc", "empty bearer": "Bearer ", "bare bearer": "Bearer", "bearer spaces": "Bearer    ", "token only": raw} {
		if w := serve(h, "Authorization", v); w.Code != 401 {
			t.Errorf("%s: Authorization = %d", name, w.Code)
		}
	}
	if w := serve(h, "x-api-key", "   "); w.Code != 200 {
		t.Errorf("a blank x-api-key and nothing else = %d, want the open gateway to pass it", w.Code)
	}
}

func TestXAPIKeyWinsOverAuthorizationAndABlankOneFallsThrough(t *testing.T) {
	ks, raw, admin, _ := keys(t)
	var seen context.Context
	h := auth.KeyAuth(auth.KeyAuthOptions{Keys: ks, Required: true})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = r.Context() }))
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	r.Header.Set("x-api-key", raw)
	r.Header.Set("Authorization", "Bearer "+admin)
	if auth.RawKey(r) != raw {
		t.Fatal("x-api-key must win over Authorization")
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
	if scopes, _ := pipeline.Scopes(seen); len(scopes) != 1 || scopes[0] != "completions" {
		t.Fatal("the request was authenticated with the Authorization key, not x-api-key")
	}
	r.Header.Set("x-api-key", "  ")
	if auth.RawKey(r) != admin {
		t.Fatal("a blank x-api-key must fall through to Authorization")
	}
}

func TestAMalformedOrNearlyRightKeyIs401(t *testing.T) {
	ks, raw, _, _ := keys(t)
	h := auth.KeyAuth(auth.KeyAuthOptions{Keys: ks, Required: true})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for name, v := range map[string]string{
		"non-utf8":           "nxs_\xff\xfe\xfd" + strings.Repeat("a", 61),
		"non-utf8 short":     "\xff\xfe\xfd\xfc\xfb\xfa\xf9\xf8\xf7\xf6\xf5\xf4\xf3",
		"right prefix, tail": raw[:12] + strings.Repeat("0", len(raw)-12),
	} {
		if w := serve(h, "x-api-key", v); w.Code != 401 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
}

func TestAnExpiredKeyIs401(t *testing.T) {
	now := time.Now()
	s := store.NewMemory()
	tn, err := tenant.NewService(s.Tenants()).Create(context.Background(), &tenant.CreateInput{Name: "A", Slug: "a"})
	if err != nil {
		t.Fatal(err)
	}
	ks := key.NewService(s.Keys(), key.WithClock(func() time.Time { return now }))
	exp := now.Add(time.Hour)
	_, raw, err := ks.Create(context.Background(), &key.CreateInput{TenantID: tn.ID.String(), Name: "k", ExpiresAt: &exp})
	if err != nil {
		t.Fatal(err)
	}
	h := auth.KeyAuth(auth.KeyAuthOptions{Keys: ks, Required: true})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if w := serve(h, "x-api-key", raw); w.Code != 200 {
		t.Fatalf("before expiry = %d", w.Code)
	}
	now = now.Add(2 * time.Hour)
	if w := serve(h, "x-api-key", raw); w.Code != 401 {
		t.Fatalf("expired = %d", w.Code)
	}
}

func TestChangingTheScopesInTheContextDoesNotChangeTheKey(t *testing.T) {
	ks, raw, _, k := keys(t)
	ctx, err := auth.Authenticate(context.Background(), ks, raw)
	if err != nil {
		t.Fatal(err)
	}
	scopes, _ := pipeline.Scopes(ctx)
	scopes[0] = "admin"
	got, err := ks.Get(context.Background(), k.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	if got.Scopes[0] != "completions" {
		t.Fatal("a stage that edits the scopes in its context changed the stored key")
	}
}

func TestKeyAuthWithoutKeysPanicsAtConstruction(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("KeyAuth(KeyAuthOptions{}) must panic")
		}
	}()
	auth.KeyAuth(auth.KeyAuthOptions{})
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

func TestWriteErrorNeverEchoesAnInternalError(t *testing.T) {
	w := httptest.NewRecorder()
	auth.WriteError(w, errors.New("dsn=postgres://u:p@h/db"))
	if w.Code != 500 {
		t.Fatalf("plain error = %d", w.Code)
	}
	if contains(w.Body.String(), "postgres://") || !contains(w.Body.String(), `"message":"internal error"`) {
		t.Fatalf("body = %s", w.Body)
	}
	if w.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("only a 401 carries WWW-Authenticate")
	}
}

func TestWriteErrorWritesOnlyTheRefusalsTextForAWrappedRefusal(t *testing.T) {
	ref := &pipeline.RefusalError{Code: pipeline.CodeForbidden, Status: 403, Message: "no"}
	w := httptest.NewRecorder()
	auth.WriteError(w, fmt.Errorf("upstream https://secret.example/v1 said: %w", ref))
	if w.Code != 403 || contains(w.Body.String(), "secret.example") || !contains(w.Body.String(), `"message":"nexus: no"`) {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
}

func TestA401CarriesWWWAuthenticate(t *testing.T) {
	ks, _, _, _ := keys(t)
	h := auth.KeyAuth(auth.KeyAuthOptions{Keys: ks, Required: true})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if w := serve(h, "", ""); w.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q", w.Header().Get("WWW-Authenticate"))
	}
}

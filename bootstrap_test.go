package nexus_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/api"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
)

// bootstrapKey is the raw value an operator supplies in config.
var bootstrapKey = "nxs_" + strings.Repeat("cd", 32)

// captureLogger keeps every line it is given, flattened, so a test can look
// for a secret in any message or field.
type captureLogger struct {
	mu    sync.Mutex
	lines []string
}

func (c *captureLogger) add(level, msg string, args []any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, level+" "+msg+" "+fmt.Sprint(args...))
}
func (c *captureLogger) Debug(m string, a ...any) { c.add("debug", m, a) }
func (c *captureLogger) Info(m string, a ...any)  { c.add("info", m, a) }
func (c *captureLogger) Warn(m string, a ...any)  { c.add("warn", m, a) }
func (c *captureLogger) Error(m string, a ...any) { c.add("error", m, a) }
func (c *captureLogger) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.lines, "\n")
}

func bootstrapGateway(t *testing.T, opts ...nexus.Option) *nexus.Gateway {
	t.Helper()
	gw := nexus.New(append([]nexus.Option{nexus.WithDatabase(store.NewMemory())}, opts...)...)
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gw.Shutdown(context.Background()) })
	return gw
}

func TestBootstrapKeyIsIdempotentAndOpensTheAdminAPI(t *testing.T) {
	ctx := context.Background()
	gw := bootstrapGateway(t, nexus.WithBootstrapAdminKey(bootstrapKey))
	for i := range 2 {
		if err := gw.EnsureBootstrapAdminKey(ctx); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	tenants, err := gw.Tenants().List(ctx, &tenant.ListOptions{})
	if err != nil || len(tenants.Items) != 1 || tenants.Items[0].Slug != "operator" || tenants.Items[0].Name != "Operator" {
		t.Fatalf("tenants = %+v (err %v), want exactly one named Operator with slug operator", tenants, err)
	}
	keys, err := gw.Keys().List(ctx, tenants.Items[0].ID.String())
	if err != nil || len(keys) != 1 {
		t.Fatalf("%d keys (err %v), want 1", len(keys), err)
	}
	if k := keys[0]; k.Name != "bootstrap admin" || len(k.Scopes) != 1 || k.Scopes[0] != key.ScopeAdmin || k.Status != key.KeyActive {
		t.Fatalf("key = name %q scopes %v status %s", k.Name, k.Scopes, k.Status)
	}

	srv := httptest.NewServer(api.New(gw).Handler())
	defer srv.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/admin/providers", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("x-api-key", bootstrapKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/providers with the bootstrap key = %d, want 200", resp.StatusCode)
	}
}

func TestBootstrapDoesNothingWithoutAKey(t *testing.T) {
	ctx := context.Background()
	gw := bootstrapGateway(t)
	if err := gw.EnsureBootstrapAdminKey(ctx); err != nil {
		t.Fatal(err)
	}
	if res, err := gw.Tenants().List(ctx, &tenant.ListOptions{}); err != nil || len(res.Items) != 0 {
		t.Fatalf("tenants = %+v (err %v), want none", res, err)
	}
}

func TestBootstrapReusesAnExistingOperatorTenant(t *testing.T) {
	ctx := context.Background()
	gw := bootstrapGateway(t, nexus.WithBootstrapAdminKey(bootstrapKey))
	mine, err := gw.Tenants().Create(ctx, &tenant.CreateInput{Name: "Mine", Slug: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if err = gw.EnsureBootstrapAdminKey(ctx); err != nil {
		t.Fatal(err)
	}
	k, err := gw.Keys().Validate(ctx, bootstrapKey)
	if err != nil || k.TenantID != mine.ID {
		t.Fatalf("validate = %v; want a key on tenant %s", err, mine.ID)
	}
	if res, _ := gw.Tenants().List(ctx, &tenant.ListOptions{}); len(res.Items) != 1 {
		t.Fatalf("%d tenants, want 1", len(res.Items))
	}
}

func TestBootstrapRefusesAnInactiveOperatorTenant(t *testing.T) {
	ctx := context.Background()
	gw := bootstrapGateway(t, nexus.WithBootstrapAdminKey(bootstrapKey))
	op, err := gw.Tenants().Create(ctx, &tenant.CreateInput{Name: "Operator", Slug: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if err = gw.Tenants().SetStatus(ctx, op.ID.String(), tenant.StatusSuspended); err != nil {
		t.Fatal(err)
	}
	err = gw.EnsureBootstrapAdminKey(ctx)
	if err == nil {
		t.Fatal("an inactive operator tenant must be an error")
	}
	if leaks, names := strings.Contains(err.Error(), bootstrapKey), strings.Contains(err.Error(), "operator"); leaks || !names {
		t.Fatalf("the error carries the key = %v, names the tenant = %v", leaks, names)
	}
	if _, err := gw.Keys().Validate(ctx, bootstrapKey); err == nil {
		t.Fatal("no key may be created on an inactive tenant")
	}
}

func TestBootstrapRejectsAMalformedKeyWithoutEchoingIt(t *testing.T) {
	const bad = "swordfish-not-a-key"
	gw := bootstrapGateway(t, nexus.WithBootstrapAdminKey(bad))
	err := gw.EnsureBootstrapAdminKey(context.Background())
	if err == nil {
		t.Fatal("a malformed bootstrap key must be an error")
	}
	if strings.Contains(err.Error(), bad) {
		t.Fatal("the error echoes the key")
	}
	if !errors.Is(err, key.ErrInvalid) || !strings.Contains(err.Error(), fmt.Sprintf("%d characters", len(bad))) {
		t.Fatalf("the error is invalid-input = %v, gives the length %d = %v", errors.Is(err, key.ErrInvalid), len(bad), strings.Contains(err.Error(), "characters"))
	}
	// A bad key must not leave an operator tenant behind.
	if res, _ := gw.Tenants().List(context.Background(), &tenant.ListOptions{}); len(res.Items) != 0 {
		t.Fatalf("%d tenants after a refused key, want none", len(res.Items))
	}
}

func TestBootstrapWarnsAboutARevokedKeyAndNeverLogsTheRawKey(t *testing.T) {
	ctx := context.Background()
	log := &captureLogger{}
	gw := bootstrapGateway(t, nexus.WithBootstrapAdminKey(bootstrapKey), nexus.WithLogger(log))
	if err := gw.EnsureBootstrapAdminKey(ctx); err != nil {
		t.Fatal(err)
	}
	k, err := gw.Keys().Validate(ctx, bootstrapKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = gw.Keys().Revoke(ctx, k.ID.String()); err != nil {
		t.Fatal(err)
	}
	if err = gw.EnsureBootstrapAdminKey(ctx); err != nil {
		t.Fatalf("a revoked bootstrap key is a warning, not an error: %v", err)
	}
	if _, err = gw.Keys().Validate(ctx, bootstrapKey); err == nil {
		t.Fatal("the revoked key must stay revoked")
	}
	out := log.text()
	if strings.Contains(out, bootstrapKey) || strings.Contains(out, bootstrapKey[len(bootstrapKey)-40:]) {
		t.Fatal("the log carries the raw key")
	}
	if !strings.Contains(out, "warn") || !strings.Contains(out, k.ID.String()) {
		t.Fatalf("want a warning that carries the key id %s; log:\n%s", k.ID, out)
	}
}

// wantWarnWithKeyID fails unless the log holds a warning that names keyID,
// and never prints the raw key.
func wantWarnWithKeyID(t *testing.T, log *captureLogger, keyID string) {
	t.Helper()
	out := log.text()
	if strings.Contains(out, bootstrapKey) || strings.Contains(out, bootstrapKey[len(bootstrapKey)-40:]) {
		t.Fatal("the log carries the raw key")
	}
	var warned bool
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "warn ") && strings.Contains(line, keyID) {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("want a warning that carries the key id %s; log:\n%s", keyID, out)
	}
}

func TestBootstrapWarnsAboutAnExistingKeyWithoutTheAdminScope(t *testing.T) {
	ctx := context.Background()
	log := &captureLogger{}
	gw := bootstrapGateway(t, nexus.WithBootstrapAdminKey(bootstrapKey), nexus.WithLogger(log))
	op, err := gw.Tenants().Create(ctx, &tenant.CreateInput{Name: "Operator", Slug: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	k, _, err := gw.Keys().Ensure(ctx, bootstrapKey, &key.CreateInput{TenantID: op.ID.String(), Name: "weak"})
	if err != nil {
		t.Fatal(err)
	}
	if err = gw.EnsureBootstrapAdminKey(ctx); err != nil {
		t.Fatal(err)
	}
	wantWarnWithKeyID(t, log, k.ID.String())
}

func TestBootstrapWarnsAboutAnExistingKeyUnderAnotherTenant(t *testing.T) {
	ctx := context.Background()
	log := &captureLogger{}
	gw := bootstrapGateway(t, nexus.WithBootstrapAdminKey(bootstrapKey), nexus.WithLogger(log))
	other, err := gw.Tenants().Create(ctx, &tenant.CreateInput{Name: "Other", Slug: "other"})
	if err != nil {
		t.Fatal(err)
	}
	k, _, err := gw.Keys().Ensure(ctx, bootstrapKey, &key.CreateInput{TenantID: other.ID.String(), Name: "elsewhere", Scopes: []string{key.ScopeAdmin}})
	if err != nil {
		t.Fatal(err)
	}
	if err = gw.EnsureBootstrapAdminKey(ctx); err != nil {
		t.Fatal(err)
	}
	wantWarnWithKeyID(t, log, k.ID.String())
}

func TestBootstrapStaysQuietForItsOwnHealthyKey(t *testing.T) {
	ctx := context.Background()
	log := &captureLogger{}
	gw := bootstrapGateway(t, nexus.WithBootstrapAdminKey(bootstrapKey), nexus.WithLogger(log))
	for range 2 {
		if err := gw.EnsureBootstrapAdminKey(ctx); err != nil {
			t.Fatal(err)
		}
	}
	text := log.text()
	if strings.Contains(text, bootstrapKey) {
		t.Fatal("the log holds the raw bootstrap key")
	}
	// Only now is it safe to print the log.
	if strings.Contains(text, "warn ") {
		t.Fatalf("a healthy bootstrap key warned:\n%s", text)
	}
}

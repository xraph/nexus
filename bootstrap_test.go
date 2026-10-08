package nexus_test

import (
	"context"
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
	if strings.Contains(err.Error(), bootstrapKey) || !strings.Contains(err.Error(), "operator") {
		t.Fatalf("error = %q: it must name the tenant and never carry the key", err)
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
		t.Fatalf("the error echoes the key: %v", err)
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
	if !strings.Contains(out, "warn") || !strings.Contains(out, k.ID.String()) {
		t.Fatalf("want a warning that carries the key id %s; log:\n%s", k.ID, out)
	}
	if strings.Contains(out, bootstrapKey) || strings.Contains(out, bootstrapKey[len(bootstrapKey)-40:]) {
		t.Fatal("the log carries the raw key")
	}
}

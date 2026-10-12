package api_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

const storeDown = "store down at postgres://u:secret@db/x"

// brokenKeys, brokenTenants and brokenUsage fail their reads with an error
// that carries a DSN.
type brokenKeys struct{ key.Service }

func (brokenKeys) List(context.Context, string) ([]*key.APIKey, error) {
	return nil, errors.New(storeDown)
}
func (brokenKeys) Revoke(context.Context, string) error { return errors.New(storeDown) }

type brokenTenants struct{ tenant.Service }

func (brokenTenants) List(context.Context, *tenant.ListOptions) (*tenant.ListResult, error) {
	return nil, errors.New(storeDown)
}
func (brokenTenants) Delete(context.Context, string) error { return errors.New(storeDown) }

type brokenUsage struct{ usage.Service }

func (brokenUsage) Summary(context.Context, string, string) (*usage.Summary, error) {
	return nil, errors.New(storeDown)
}

func TestAdminInputErrorsAre400sWithTheirMessage(t *testing.T) {
	srv, _, _, admin := newAPI(t)
	for _, c := range []struct{ path, body, says string }{
		{"/admin/keys", `{"name":"","tenant_id":"x"}`, "name is required"},
		{"/admin/keys", `{"name":"k","tenant_id":"not-an-id"}`, "tenant id"},
		{"/admin/keys", `{"name":"k","tenant_id":"` + id.NewTenantID().String() + `","scopes":["completion"]}`, "unknown scope"},
		{"/admin/tenants", `{"name":"","slug":"x"}`, "name is required"},
		{"/admin/tenants", `{"name":"x","slug":""}`, "slug is required"},
	} {
		got := send(t, srv, "POST", c.path, admin, c.body)
		wantRefusal(t, got, 400, "invalid_request")
		if !strings.Contains(got.body, c.says) {
			t.Fatalf("POST %s %s: body %s; want it to say %q", c.path, c.body, got.shown, c.says)
		}
	}
}

func TestAdminNotFoundIs404(t *testing.T) {
	srv, _, _, admin := newAPI(t)
	ghost := id.NewTenantID().String()
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/admin/keys", `{"name":"k","tenant_id":"` + ghost + `"}`},
		{"DELETE", "/admin/keys/" + id.NewKeyID().String(), ""},
		{"GET", "/admin/tenants/" + ghost, ""},
		{"PATCH", "/admin/tenants/" + ghost, `{"name":"y"}`},
	} {
		wantRefusal(t, send(t, srv, c.method, c.path, admin, c.body), 404, "not_found")
	}
}

func TestAdminStoreFailuresAreAFixed500AndLogged(t *testing.T) {
	s := store.NewMemory()
	logs := &recordingLogger{}
	srv, _, _, admin := newAPI(t, nexus.WithDatabase(s), nexus.WithLogger(logs),
		nexus.WithKeyService(brokenKeys{key.NewService(s.Keys(), key.WithTenants(s.Tenants()))}),
		nexus.WithTenantService(brokenTenants{tenant.NewService(s.Tenants())}),
		nexus.WithUsageService(brokenUsage{usage.NewService(s.Usage())}))
	for _, c := range []struct{ method, path string }{
		{"GET", "/admin/keys?tenant_id=" + id.NewTenantID().String()},
		{"DELETE", "/admin/keys/" + id.NewKeyID().String()},
		{"GET", "/admin/tenants"},
		{"DELETE", "/admin/tenants/" + id.NewTenantID().String()},
		{"GET", "/admin/usage?tenant_id=" + id.NewTenantID().String()},
	} {
		got := send(t, srv, c.method, c.path, admin, "")
		wantRefusal(t, got, 500, "internal_error")
		if strings.Contains(got.body, "postgres://") || strings.Contains(got.body, "secret") || !strings.Contains(got.body, "internal error") {
			t.Fatalf("%s %s: body %s; want the fixed internal error", c.method, c.path, got.shown)
		}
	}
	if out := logs.text(); strings.Count(out, "store down") != 5 {
		t.Fatalf("logged %q; want each of the five causes logged", out)
	}
}

func TestDeletingATenantThatHasKeysIs409AndLeavesItInPlace(t *testing.T) {
	srv, gw, _, admin := newAPI(t)
	ctx := context.Background()
	keyed, err := gw.Tenants().Create(ctx, &tenant.CreateInput{Name: "Keyed", Slug: "keyed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = gw.Keys().Create(ctx, &key.CreateInput{TenantID: keyed.ID.String(), Name: "k"}); err != nil {
		t.Fatal(err)
	}
	bare, err := gw.Tenants().Create(ctx, &tenant.CreateInput{Name: "Bare", Slug: "bare"})
	if err != nil {
		t.Fatal(err)
	}

	got := send(t, srv, "DELETE", "/admin/tenants/"+keyed.ID.String(), admin, "")
	wantRefusal(t, got, 409, "conflict")
	if !strings.Contains(got.body, "disable it instead") {
		t.Fatalf("body %s; want it to say to disable the tenant", got.shown)
	}
	wantRefusal(t, send(t, srv, "GET", "/admin/tenants/"+keyed.ID.String(), admin, ""), 200, "")

	if got = send(t, srv, "DELETE", "/admin/tenants/"+bare.ID.String(), admin, ""); got.status != 204 {
		t.Fatalf("delete of a tenant without keys = %d %s; want 204", got.status, got.shown)
	}
	wantRefusal(t, send(t, srv, "GET", "/admin/tenants/"+bare.ID.String(), admin, ""), 404, "not_found")
}

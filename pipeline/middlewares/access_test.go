package middlewares_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
)

type accessFixture struct {
	mw      *middlewares.AccessMiddleware
	tenants tenant.Service
	keys    key.Service
	active  *tenant.Tenant
}

func newAccessFixture(t *testing.T) *accessFixture {
	t.Helper()
	s := store.NewMemory()
	ts := tenant.NewService(s.Tenants())
	ks := key.NewService(s.Keys(), key.WithTenants(s.Tenants()))
	tn, err := ts.Create(context.Background(), &tenant.CreateInput{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	return &accessFixture{mw: middlewares.NewAccess(ts, ks), tenants: ts, keys: ks, active: tn}
}

func (f *accessFixture) run(ctx context.Context, typ pipeline.RequestType) (context.Context, error) {
	var seen context.Context
	req := &pipeline.Request{Type: typ, Completion: &provider.CompletionRequest{Model: "m"}, State: map[string]any{}}
	if typ == pipeline.RequestEmbedding {
		req.Completion, req.Embedding = nil, &provider.EmbeddingRequest{Model: "e"}
	}
	_, err := f.mw.Process(ctx, req, func(c context.Context) (*pipeline.Response, error) {
		seen = c
		return &pipeline.Response{}, nil
	})
	return seen, err
}

func code(err error) string { _, c := pipeline.HTTPStatus(err); return c }

func errorsAs(err error, target any) bool { return errors.As(err, target) }

func TestAnActiveTenantPassesAndIsPublished(t *testing.T) {
	f := newAccessFixture(t)
	seen, err := f.run(pipeline.WithTenantID(context.Background(), f.active.ID.String()), pipeline.RequestCompletion)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := middlewares.TenantFromContext(seen)
	if !ok || got.ID != f.active.ID {
		t.Fatal("the tenant must be in the context for the quota stage")
	}
}

func TestADisabledOrUnknownTenantIsForbidden(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	for _, st := range []tenant.Status{tenant.StatusSuspended, tenant.StatusDisabled} {
		if err := f.tenants.SetStatus(ctx, f.active.ID.String(), st); err != nil {
			t.Fatal(err)
		}
		_, err := f.run(pipeline.WithTenantID(ctx, f.active.ID.String()), pipeline.RequestCompletion)
		var re *pipeline.RefusalError
		if !errorsAs(err, &re) || re.Code != pipeline.CodeForbidden || re.Status != 403 || re.Unattributed {
			t.Fatalf("%s = %v; want forbidden, 403 and attributed", st, err)
		}
	}
	_, err := f.run(pipeline.WithTenantID(ctx, id.NewTenantID().String()), pipeline.RequestCompletion)
	var re *pipeline.RefusalError
	if !errorsAs(err, &re) || re.Code != pipeline.CodeForbidden || !re.Unattributed {
		t.Fatalf("unknown tenant = %v; want forbidden and unattributed", err)
	}
}

func TestScopesComeFromTheEdgeOrTheKey(t *testing.T) {
	f := newAccessFixture(t)
	ctx := pipeline.WithTenantID(context.Background(), f.active.ID.String())
	k, _, err := f.keys.Create(ctx, &key.CreateInput{TenantID: f.active.ID.String(), Name: "embed-only", Scopes: []string{"embeddings"}})
	if err != nil {
		t.Fatal(err)
	}
	withKey := pipeline.WithKeyID(ctx, k.ID.String())
	if _, err := f.run(withKey, pipeline.RequestCompletion); code(err) != pipeline.CodeForbidden {
		t.Fatalf("completion with an embeddings-only key = %v", err)
	}
	if _, err := f.run(withKey, pipeline.RequestEmbedding); err != nil {
		t.Fatalf("embedding = %v", err)
	}
	if _, err := f.run(pipeline.WithScopes(withKey, []string{"completions"}), pipeline.RequestStream); err != nil {
		t.Fatalf("edge scopes win: %v", err)
	}
	_ = f.keys.Revoke(ctx, k.ID.String())
	if _, err := f.run(withKey, pipeline.RequestEmbedding); code(err) != pipeline.CodeUnauthenticated {
		t.Fatalf("revoked key named in process = %v", err)
	}
}

func TestAnUnattributedRequestIsNotChecked(t *testing.T) {
	f := newAccessFixture(t)
	if _, err := f.run(context.Background(), pipeline.RequestCompletion); err != nil {
		t.Fatal(err)
	}
}

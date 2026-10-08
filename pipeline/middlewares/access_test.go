package middlewares_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	if err := f.keys.Revoke(ctx, k.ID.String()); err != nil {
		t.Fatal(err)
	}
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

type stubTenants struct {
	t   *tenant.Tenant
	err error
}

func (s stubTenants) Get(context.Context, string) (*tenant.Tenant, error) { return s.t, s.err }

type stubKeys struct {
	k   *key.APIKey
	err error
}

func (s stubKeys) Get(context.Context, string) (*key.APIKey, error) { return s.k, s.err }

func wantRefusal(t *testing.T, err error, wantCode string, wantStatus int) {
	t.Helper()
	var re *pipeline.RefusalError
	if !errorsAs(err, &re) {
		t.Fatalf("err = %v; want a RefusalError", err)
	}
	if re.Code != wantCode || re.Status != wantStatus {
		t.Fatalf("refusal = %s/%d; want %s/%d", re.Code, re.Status, wantCode, wantStatus)
	}
}

func TestAKeyThatCannotBeUsedIsRefused(t *testing.T) {
	f := newAccessFixture(t)
	ctx := pipeline.WithTenantID(context.Background(), f.active.ID.String())

	other, err := f.tenants.Create(context.Background(), &tenant.CreateInput{Name: "Other", Slug: "other"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, _, err := f.keys.Create(ctx, &key.CreateInput{TenantID: other.ID.String(), Name: "theirs", Scopes: []string{"completions"}})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("another tenant's key is forbidden", func(t *testing.T) {
		_, err := f.run(pipeline.WithKeyID(ctx, foreign.ID.String()), pipeline.RequestCompletion)
		wantRefusal(t, err, pipeline.CodeForbidden, 403)
	})
	t.Run("an unknown key is unauthenticated", func(t *testing.T) {
		_, err := f.run(pipeline.WithKeyID(ctx, id.NewKeyID().String()), pipeline.RequestCompletion)
		wantRefusal(t, err, pipeline.CodeUnauthenticated, 401)
	})
	t.Run("a key with no matching scope is forbidden", func(t *testing.T) {
		k, _, err := f.keys.Create(ctx, &key.CreateInput{TenantID: f.active.ID.String(), Name: "models-only", Scopes: []string{"models"}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.run(pipeline.WithKeyID(ctx, k.ID.String()), pipeline.RequestCompletion)
		wantRefusal(t, err, pipeline.CodeForbidden, 403)
	})
}

func TestAnExpiredKeyIsUnauthenticated(t *testing.T) {
	s := store.NewMemory()
	ts := tenant.NewService(s.Tenants())
	now := time.Now()
	ks := key.NewService(s.Keys(), key.WithTenants(s.Tenants()), key.WithClock(func() time.Time { return now }))
	tn, err := ts.Create(context.Background(), &tenant.CreateInput{Name: "Acme", Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	exp := now.Add(time.Hour)
	k, _, err := ks.Create(context.Background(), &key.CreateInput{TenantID: tn.ID.String(), Name: "short", Scopes: []string{"completions"}, ExpiresAt: &exp})
	if err != nil {
		t.Fatal(err)
	}
	f := &accessFixture{mw: middlewares.NewAccess(ts, ks), tenants: ts, keys: ks, active: tn}
	ctx := pipeline.WithKeyID(pipeline.WithTenantID(context.Background(), tn.ID.String()), k.ID.String())
	if _, err = f.run(ctx, pipeline.RequestCompletion); err != nil {
		t.Fatalf("before expiry = %v", err)
	}
	now = now.Add(2 * time.Hour)
	_, err = f.run(ctx, pipeline.RequestCompletion)
	wantRefusal(t, err, pipeline.CodeUnauthenticated, 401)
}

func TestALookupThatFailsIsUnavailableAndKeepsItsCause(t *testing.T) {
	f := newAccessFixture(t)
	sentinel := errors.New("store down")
	ctx := pipeline.WithTenantID(context.Background(), f.active.ID.String())
	withKey := pipeline.WithKeyID(ctx, id.NewKeyID().String())

	t.Run("tenant", func(t *testing.T) {
		mw := middlewares.NewAccess(stubTenants{err: sentinel}, f.keys)
		_, err := (&accessFixture{mw: mw}).run(ctx, pipeline.RequestCompletion)
		wantRefusal(t, err, pipeline.CodeUnavailable, 503)
		if !errors.Is(err, sentinel) {
			t.Fatalf("the cause is lost: %v", err)
		}
	})
	t.Run("key", func(t *testing.T) {
		mw := middlewares.NewAccess(f.tenants, stubKeys{err: sentinel})
		_, err := (&accessFixture{mw: mw}).run(withKey, pipeline.RequestCompletion)
		wantRefusal(t, err, pipeline.CodeUnavailable, 503)
		if !errors.Is(err, sentinel) {
			t.Fatalf("the cause is lost: %v", err)
		}
	})
}

func TestANilLookupFailsClosed(t *testing.T) {
	f := newAccessFixture(t)
	ctx := pipeline.WithTenantID(context.Background(), f.active.ID.String())
	withKey := pipeline.WithKeyID(ctx, id.NewKeyID().String())

	t.Run("tenant", func(t *testing.T) {
		mw := middlewares.NewAccess(stubTenants{}, f.keys)
		_, err := (&accessFixture{mw: mw}).run(ctx, pipeline.RequestCompletion)
		wantRefusal(t, err, pipeline.CodeUnavailable, 503)
	})
	t.Run("key", func(t *testing.T) {
		mw := middlewares.NewAccess(f.tenants, stubKeys{})
		_, err := (&accessFixture{mw: mw}).run(withKey, pipeline.RequestCompletion)
		wantRefusal(t, err, pipeline.CodeUnavailable, 503)
	})
}

func TestEdgeScopesAreEnforcedEvenWithoutAKeyID(t *testing.T) {
	f := newAccessFixture(t)
	ctx := pipeline.WithTenantID(context.Background(), f.active.ID.String())
	_, err := f.run(pipeline.WithScopes(ctx, []string{"embeddings"}), pipeline.RequestCompletion)
	wantRefusal(t, err, pipeline.CodeForbidden, 403)
	if _, err := f.run(pipeline.WithScopes(ctx, []string{"embeddings"}), pipeline.RequestEmbedding); err != nil {
		t.Fatalf("matching edge scope with no key id = %v", err)
	}
}

func TestEmptyEdgeScopesGrantNothing(t *testing.T) {
	f := newAccessFixture(t)
	ctx := pipeline.WithTenantID(context.Background(), f.active.ID.String())
	k, _, err := f.keys.Create(ctx, &key.CreateInput{TenantID: f.active.ID.String(), Name: "full", Scopes: []string{"completions", "embeddings"}})
	if err != nil {
		t.Fatal(err)
	}
	withKey := pipeline.WithKeyID(ctx, k.ID.String())
	for _, typ := range []pipeline.RequestType{pipeline.RequestCompletion, pipeline.RequestStream, pipeline.RequestEmbedding} {
		_, err := f.run(pipeline.WithScopes(withKey, []string{}), typ)
		wantRefusal(t, err, pipeline.CodeForbidden, 403)
	}
}

func TestAKeyIDWithoutATenantIsRefused(t *testing.T) {
	f := newAccessFixture(t)
	ctx := pipeline.WithKeyID(context.Background(), id.NewKeyID().String())
	_, err := f.run(ctx, pipeline.RequestCompletion)
	wantRefusal(t, err, pipeline.CodeInvalidRequest, 400)
	var r *pipeline.RefusalError
	if !errorsAs(err, &r) || !r.Unattributed {
		t.Fatalf("refusal %v; want it recorded unattributed, since its key names no tenant", err)
	}
}

// modelSeenByNext runs the access stage for a tenant whose default model is
// def and returns the model the next stage saw.
func modelSeenByNext(t *testing.T, def string, req *pipeline.Request) string {
	t.Helper()
	tn := &tenant.Tenant{ID: id.NewTenantID(), Status: tenant.StatusActive, Config: tenant.Config{DefaultModel: def}}
	mw := middlewares.NewAccess(stubTenants{t: tn}, stubKeys{})
	var seen string
	_, err := mw.Process(pipeline.WithTenantID(context.Background(), tn.ID.String()), req, func(context.Context) (*pipeline.Response, error) {
		switch {
		case req.Completion != nil:
			seen = req.Completion.Model
		case req.Embedding != nil:
			seen = req.Embedding.Model
		}
		return &pipeline.Response{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return seen
}

func TestTheTenantsDefaultModelFillsAnEmptyModel(t *testing.T) {
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{}, State: map[string]any{}}
	if got := modelSeenByNext(t, "gpt-4o", req); got != "gpt-4o" {
		t.Fatalf("model = %q; want the tenant's default", got)
	}
	emb := &pipeline.Request{Type: pipeline.RequestEmbedding, Embedding: &provider.EmbeddingRequest{}, State: map[string]any{}}
	if got := modelSeenByNext(t, "text-embedding-3-small", emb); got != "text-embedding-3-small" {
		t.Fatalf("embedding model = %q; want the tenant's default", got)
	}
}

func TestARequestThatNamesAModelKeepsIt(t *testing.T) {
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{Model: "o1"}, State: map[string]any{}}
	if got := modelSeenByNext(t, "gpt-4o", req); got != "o1" {
		t.Fatalf("model = %q; want the requested o1", got)
	}
}

func TestARequestWithNoModelAndNoDefaultIsRefused(t *testing.T) {
	cases := map[string]*pipeline.Request{
		"completion": {Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{}, State: map[string]any{}},
		"embedding":  {Type: pipeline.RequestEmbedding, Embedding: &provider.EmbeddingRequest{}, State: map[string]any{}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			tn := &tenant.Tenant{ID: id.NewTenantID(), Status: tenant.StatusActive}
			mw := middlewares.NewAccess(stubTenants{t: tn}, stubKeys{})
			reached := false
			_, err := mw.Process(pipeline.WithTenantID(context.Background(), tn.ID.String()), req, func(context.Context) (*pipeline.Response, error) {
				reached = true
				return &pipeline.Response{}, nil
			})
			wantRefusal(t, err, pipeline.CodeInvalidRequest, 400)
			if reached {
				t.Fatal("a request with no model must not reach next")
			}
			if !strings.Contains(err.Error(), "model is required") {
				t.Fatalf("message %q; want model is required", err.Error())
			}
		})
	}
}

func TestAnUnattributedRequestWithNoModelIsRefused(t *testing.T) {
	f := newAccessFixture(t)
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{}, State: map[string]any{}}
	_, err := f.mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		t.Fatal("a request with no model must not reach next")
		return nil, nil //nolint:nilnil // unreachable
	})
	wantRefusal(t, err, pipeline.CodeInvalidRequest, 400)
}

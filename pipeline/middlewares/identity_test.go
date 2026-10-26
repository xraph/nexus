package middlewares_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/usage"
)

func runOnce(ctx context.Context, t *testing.T, mw pipeline.Middleware, req *pipeline.Request) (context.Context, error) {
	t.Helper()
	var seen context.Context
	_, err := mw.Process(ctx, req, func(c context.Context) (*pipeline.Response, error) {
		seen = c
		return &pipeline.Response{}, nil
	})
	return seen, err
}

func TestRequestIDIsSetOnce(t *testing.T) {
	req := &pipeline.Request{State: map[string]any{}}
	seen, _ := runOnce(context.Background(), t, middlewares.NewRequestID(), req)
	if _, err := id.ParseRequestID(pipeline.RequestID(seen)); err != nil {
		t.Fatalf("request id %q: %v", pipeline.RequestID(seen), err)
	}
	given := id.NewRequestID().String()
	seen, _ = runOnce(pipeline.WithRequestID(context.Background(), given), t, middlewares.NewRequestID(), req)
	if pipeline.RequestID(seen) != given {
		t.Fatalf("an existing request id must be kept")
	}
}

func TestIdentityReconcilesContextAndRequest(t *testing.T) {
	tenant, other, key := id.NewTenantID().String(), id.NewTenantID().String(), id.NewKeyID().String()
	completion := func(t, k string) *pipeline.Request {
		return &pipeline.Request{Completion: &provider.CompletionRequest{TenantID: t, KeyID: k}, State: map[string]any{}}
	}
	cases := []struct {
		name    string
		ctx     context.Context
		req     *pipeline.Request
		want    string
		wantErr bool
	}{
		{"request fields only", context.Background(), completion(tenant, key), tenant, false},
		{"context only", pipeline.WithTenantID(context.Background(), tenant), completion("", ""), tenant, false},
		{"both agree", pipeline.WithTenantID(context.Background(), tenant), completion(tenant, ""), tenant, false},
		{"neither", context.Background(), completion("", ""), "", false},
		{"they disagree", pipeline.WithTenantID(context.Background(), tenant), completion(other, ""), "", true},
		{"tenant id does not parse", context.Background(), completion("acme", ""), "", true},
		{"key id does not parse", context.Background(), completion("", "nxs_abc"), "", true},
		{"embedding fields", context.Background(), &pipeline.Request{Embedding: &provider.EmbeddingRequest{TenantID: tenant}, State: map[string]any{}}, tenant, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			seen, err := runOnce(c.ctx, t, middlewares.NewIdentity(), c.req)
			if c.wantErr {
				if !errors.Is(err, middlewares.ErrInvalidIdentity) {
					t.Fatalf("err = %v, want ErrInvalidIdentity", err)
				}
				return
			}
			if err != nil || pipeline.TenantID(seen) != c.want {
				t.Fatalf("tenant in context = %q, %v; want %q", pipeline.TenantID(seen), err, c.want)
			}
			var inReq string
			if c.req.Completion != nil {
				inReq = c.req.Completion.TenantID
			} else {
				inReq = c.req.Embedding.TenantID
			}
			if inReq != c.want {
				t.Fatalf("tenant in request = %q, want %q (later stages read the request)", inReq, c.want)
			}
		})
	}
}

func TestIdentityRefusesInvalidMetadata(t *testing.T) {
	bad := map[string]string{"a.b": "x"}
	for name, req := range map[string]*pipeline.Request{
		"completion": {Completion: &provider.CompletionRequest{Metadata: bad}, State: map[string]any{}},
		"embedding":  {Embedding: &provider.EmbeddingRequest{Metadata: bad}, State: map[string]any{}},
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			_, err := middlewares.NewIdentity().Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
				called = true
				return &pipeline.Response{}, nil
			})
			if called {
				t.Fatal("next was called for a refused request")
			}
			if !errors.Is(err, usage.ErrInvalidMetadata) {
				t.Fatalf("err = %v, want one wrapping ErrInvalidMetadata", err)
			}
			if status, code := pipeline.HTTPStatus(err); status != 400 || code != pipeline.CodeInvalidRequest {
				t.Fatalf("refusal = %d %s, want 400 invalid_request", status, code)
			}
		})
	}
}

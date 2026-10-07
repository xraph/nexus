package middlewares_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
)

func runOnce(t *testing.T, ctx context.Context, mw pipeline.Middleware, req *pipeline.Request) (context.Context, error) {
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
	seen, _ := runOnce(t, context.Background(), middlewares.NewRequestID(), req)
	if _, err := id.ParseRequestID(pipeline.RequestID(seen)); err != nil {
		t.Fatalf("request id %q: %v", pipeline.RequestID(seen), err)
	}
	given := id.NewRequestID().String()
	seen, _ = runOnce(t, pipeline.WithRequestID(context.Background(), given), middlewares.NewRequestID(), req)
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
			seen, err := runOnce(t, c.ctx, middlewares.NewIdentity(), c.req)
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

package grpcsrv_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/xraph/nexus/grpcsrv"
	nexusv1 "github.com/xraph/nexus/grpcsrv/proto/nexus/v1"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/testutil"
)

// goodKey is a well-formed test key. It is not a secret: no store holds it.
const goodKey = "nxs_00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

type oneKey struct{ k *key.APIKey }

func (o oneKey) Validate(_ context.Context, raw string) (*key.APIKey, error) {
	if raw != goodKey {
		return nil, key.ErrNotFound
	}
	return o.k, nil
}

type oneTenant struct{ t *tenant.Tenant }

func (o oneTenant) Get(context.Context, string) (*tenant.Tenant, error) { return o.t, nil }

// seeingStreamer records the identity the engine was called with.
type seeingStreamer struct {
	tenant, keyID string
	scopes        []string
	called        bool
}

func (s *seeingStreamer) CompleteStream(ctx context.Context, _ *provider.CompletionRequest) (provider.Stream, error) {
	s.called = true
	s.tenant, s.keyID = pipeline.TenantID(ctx), pipeline.KeyID(ctx)
	s.scopes, _ = pipeline.Scopes(ctx)
	return testutil.NewFakeStream(nil, nil), nil
}

func authed(t *testing.T, status tenant.Status) (*seeingStreamer, nexusv1.CompletionsClient, *key.APIKey) {
	t.Helper()
	k := &key.APIKey{ID: id.NewKeyID(), TenantID: id.NewTenantID(), Scopes: []string{"completions"}, Status: key.KeyActive}
	seen := &seeingStreamer{}
	srv := grpc.NewServer(grpc.StreamInterceptor(grpcsrv.KeyAuth(oneKey{k}, oneTenant{&tenant.Tenant{ID: k.TenantID, Status: status}})))
	grpcsrv.Register(srv, seen)
	return seen, nexusv1.NewCompletionsClient(dialBuf(t, srv)), k
}

// recvErr runs one call with md and returns the error that ends it.
func recvErr(t *testing.T, client nexusv1.CompletionsClient, md metadata.MD) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.CompleteStream(metadata.NewOutgoingContext(ctx, md), &nexusv1.CompletionRequest{Model: "m"})
	if err != nil {
		return err
	}
	for {
		if _, err := stream.Recv(); err != nil {
			return err
		}
	}
}

func TestKeyAuthRefusesACallWithoutAKey(t *testing.T) {
	seen, client, _ := authed(t, tenant.StatusActive)
	for name, md := range map[string]metadata.MD{
		"none":           {},
		"basic":          metadata.Pairs("authorization", "Basic dTpw"),
		"an unknown key": metadata.Pairs("x-api-key", "nxs_"+goodKey[4:66]+"00"),
	} {
		err := recvErr(t, client, md)
		if st, _ := status.FromError(err); st.Code() != codes.Unauthenticated {
			t.Fatalf("%s: status %s, want Unauthenticated", name, st.Code())
		}
	}
	if seen.called {
		t.Fatal("the engine was called without a key")
	}
}

func TestKeyAuthPutsTheKeysIdentityInTheContext(t *testing.T) {
	for name, md := range map[string]metadata.MD{
		"x-api-key": metadata.Pairs("x-api-key", goodKey),
		"bearer":    metadata.Pairs("authorization", "Bearer "+goodKey),
	} {
		t.Run(name, func(t *testing.T) {
			seen, client, k := authed(t, tenant.StatusActive)
			if err := recvErr(t, client, md); !errors.Is(err, io.EOF) {
				t.Fatalf("call = %s", status.Code(err))
			}
			if !seen.called || seen.tenant != k.TenantID.String() || seen.keyID != k.ID.String() || len(seen.scopes) != 1 || seen.scopes[0] != "completions" {
				t.Fatalf("engine saw called %v tenant %q key %q scopes %v; want the key's identity", seen.called, seen.tenant, seen.keyID, seen.scopes)
			}
		})
	}
}

func TestKeyAuthRefusesAKeyOfASuspendedTenant(t *testing.T) {
	seen, client, _ := authed(t, tenant.StatusSuspended)
	err := recvErr(t, client, metadata.Pairs("x-api-key", goodKey))
	if st, _ := status.FromError(err); st.Code() != codes.PermissionDenied || st.Message() != "nexus: tenant is suspended" {
		t.Fatalf("status %s %q, want PermissionDenied for the suspended tenant", st.Code(), st.Message())
	}
	if seen.called {
		t.Fatal("the engine was called for a suspended tenant")
	}
}

package grpcsrv

import (
	"context"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/xraph/nexus/auth"
)

// KeyAuth returns a stream interceptor that authenticates every call with a
// gateway key, the same check the HTTP edges run. The key comes from the
// "x-api-key" metadata or "authorization: Bearer <key>". A missing or bad
// key is codes.Unauthenticated, a key whose tenant is not active is
// codes.PermissionDenied (when tenants is not nil, which it should be), and
// a store that cannot be read is codes.Unavailable, with its cause logged to
// slog.Default(). On success the handler's context carries the key's
// tenant, id and scopes, so the pipeline attributes, scopes and limits the
// call like an HTTP one.
//
//	srv := grpc.NewServer(grpc.StreamInterceptor(grpcsrv.KeyAuth(gw.Keys(), gw.Tenants())))
//	grpcsrv.Register(srv, gw.Engine())
//
// It panics when keys is nil.
func KeyAuth(keys auth.KeyValidator, tenants auth.TenantGetter) grpc.StreamServerInterceptor {
	if keys == nil {
		panic("grpcsrv: KeyAuth needs a key validator")
	}
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		md, _ := metadata.FromIncomingContext(ss.Context())
		ctx, err := auth.AuthenticateWithTenants(ss.Context(), keys, tenants, presented(md))
		if err != nil {
			auth.LogServerError(ss.Context(), slog.Default(), info.FullMethod, err)
			return statusOf(err)
		}
		return handler(srv, &authedStream{ServerStream: ss, ctx: ctx})
	}
}

// presented returns the key in x-api-key, or the Bearer token in
// authorization, or "". An authorization value that is not Bearer counts as
// no key.
func presented(md metadata.MD) string {
	if v := md.Get("x-api-key"); len(v) > 0 && strings.TrimSpace(v[0]) != "" {
		return strings.TrimSpace(v[0])
	}
	if v := md.Get("authorization"); len(v) > 0 {
		h := strings.TrimSpace(v[0])
		if len(h) >= len("bearer ") && strings.EqualFold(h[:len("bearer ")], "bearer ") {
			return strings.TrimSpace(h[len("bearer "):])
		}
	}
	return ""
}

// authedStream is a ServerStream whose context carries the identity.
type authedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authedStream) Context() context.Context { return s.ctx }

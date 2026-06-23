package api

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/hopboxdev/hopbox/internal/core/ports"
)

// principalKey carries the authenticated caller through the request context.
type principalKey struct{}

func withPrincipal(ctx context.Context, pr ports.Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, pr)
}

func principalFromCtx(ctx context.Context) (ports.Principal, bool) {
	pr, ok := ctx.Value(principalKey{}).(ports.Principal)
	return pr, ok
}

// tokenFromCtx pulls the api key from request metadata: `authorization: Bearer
// <tok>` (preferred) or a bare `hopbox-token` header.
func tokenFromCtx(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if v := md.Get("authorization"); len(v) > 0 {
		return strings.TrimSpace(strings.TrimPrefix(v[0], "Bearer "))
	}
	if v := md.Get("hopbox-token"); len(v) > 0 {
		return strings.TrimSpace(v[0])
	}
	return ""
}

func authenticate(ctx context.Context, idp ports.Identity) (ports.Principal, error) {
	tok := tokenFromCtx(ctx)
	if tok == "" {
		return ports.Principal{}, status.Error(codes.Unauthenticated, "missing api token (run `hopbox login --token <token>`)")
	}
	pr, err := idp.Authenticate(ctx, ports.Credential{Scheme: "api-key", Value: tok})
	if err != nil {
		return ports.Principal{}, status.Error(codes.Unauthenticated, "invalid api token")
	}
	return pr, nil
}

// AuthUnaryInterceptor authenticates every unary call with idp and injects the
// caller's Principal. Install it only when multi-user auth is configured; with
// no interceptor the server falls back to its default (open) principal.
func AuthUnaryInterceptor(idp ports.Identity) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		pr, err := authenticate(ctx, idp)
		if err != nil {
			return nil, err
		}
		return handler(withPrincipal(ctx, pr), req)
	}
}

// AuthStreamInterceptor is the streaming counterpart.
func AuthStreamInterceptor(idp ports.Identity) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		pr, err := authenticate(ss.Context(), idp)
		if err != nil {
			return err
		}
		return handler(srv, &principalStream{ServerStream: ss, ctx: withPrincipal(ss.Context(), pr)})
	}
}

type principalStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *principalStream) Context() context.Context { return s.ctx }

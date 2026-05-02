package jwtmw

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// contextKey es un tipo privado para evitar colisiones al guardar claims en ctx.
type contextKey int

const claimsKey contextKey = iota

// FromContext recupera los claims insertados por el interceptor.
// Devuelve nil si la request no estaba autenticada.
func FromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(claimsKey).(*Claims)
	return c
}

// SkipFunc decide si una RPC concreta NO requiere autenticación
// (ej: AuthService/Login). El interceptor la consulta antes de validar.
type SkipFunc func(fullMethod string) bool

// UnaryServerInterceptor extrae el header `authorization: Bearer <token>`,
// valida con el Verifier, y publica los Claims en el contexto.
// Si el método está en `skip`, la request pasa sin auth.
func UnaryServerInterceptor(v *Verifier, skip SkipFunc) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if skip != nil && skip(info.FullMethod) {
			return handler(ctx, req)
		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		authz := firstHeader(md, "authorization")
		if authz == "" {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}
		const prefix = "Bearer "
		if !strings.HasPrefix(authz, prefix) {
			return nil, status.Error(codes.Unauthenticated, "authorization header must be 'Bearer <token>'")
		}
		token := strings.TrimSpace(strings.TrimPrefix(authz, prefix))
		claims, err := v.Verify(token)
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "invalid token: %s", err.Error())
		}
		if claims.Type != "access" {
			return nil, status.Error(codes.Unauthenticated, "wrong token type (access required)")
		}
		ctx = context.WithValue(ctx, claimsKey, claims)
		return handler(ctx, req)
	}
}

func firstHeader(md metadata.MD, key string) string {
	vs := md.Get(key)
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

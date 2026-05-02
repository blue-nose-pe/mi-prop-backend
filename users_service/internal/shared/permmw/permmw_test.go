package permmw

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/golang-jwt/jwt/v5"

	"users_service/internal/shared/jwtmw"
)

// resolverMock controla el resultado de Has(). Permite simular allow,
// deny y errores transientes (Redis/gRPC down).
type resolverMock struct {
	allow bool
	err   error
	calls int
}

func (m *resolverMock) Has(_ context.Context, _, _ string) (bool, error) {
	m.calls++
	return m.allow, m.err
}

// ctxWithSubject monta un contexto con claims simulados (lo que jwtmw
// haría en producción tras parsear el Bearer token).
func ctxWithSubject(sub string) context.Context {
	c := &jwtmw.Claims{}
	c.RegisteredClaims = jwt.RegisteredClaims{Subject: sub}
	// El interceptor de jwtmw usa una key privada; aquí replicamos la
	// metadata mínima esperando que el contexto ya las tenga.
	return injectClaims(context.Background(), c)
}

// injectClaims expone una manera de poblar el contexto sin pasar por
// el JWT signer real. Lo mismo hace jwtmw internamente.
func injectClaims(ctx context.Context, c *jwtmw.Claims) context.Context {
	// jwtmw guarda los claims con una key privada inaccesible. Para
	// estos tests, simulamos la inyección a través de metadata gRPC
	// más un decode in-process — pero como Claims tiene exposed fields
	// suficientes, usamos un `context.WithValue` con la misma key.
	//
	// jwtmw.FromContext devuelve nil si no encontró el value; entonces
	// para que estos tests funcionen, agregamos un export helper en
	// jwtmw (PutClaims, ya creado) — aquí lo invocamos.
	return jwtmw.PutClaimsForTest(ctx, c)
}

func okHandler(_ context.Context, _ any) (any, error) { return "ok", nil }

func TestPermmw_PassThrough_WhenMethodNotMapped(t *testing.T) {
	res := &resolverMock{allow: false}
	mw := UnaryServerInterceptor(res, map[string]string{
		"/users.v1.UserService/CreateUser": "db_users.users.write",
	})

	out, err := mw(
		context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/users.v1.UserService/Me"},
		okHandler,
	)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out != "ok" {
		t.Errorf("handler should have run; got %v", out)
	}
	if res.calls != 0 {
		t.Errorf("resolver should NOT be called for unmapped methods; got %d calls", res.calls)
	}
}

func TestPermmw_Denies_WhenResolverFalse(t *testing.T) {
	res := &resolverMock{allow: false}
	mw := UnaryServerInterceptor(res, map[string]string{
		"/users.v1.UserService/CreateUser": "db_users.users.write",
	})

	ctx := ctxWithSubject("user-123")
	_, err := mw(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/users.v1.UserService/CreateUser"},
		okHandler)
	if err == nil {
		t.Fatal("expected PermissionDenied")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", status.Code(err))
	}
	if res.calls != 1 {
		t.Errorf("resolver should be called exactly once; got %d", res.calls)
	}
}

func TestPermmw_Allows_WhenResolverTrue(t *testing.T) {
	res := &resolverMock{allow: true}
	mw := UnaryServerInterceptor(res, map[string]string{
		"/users.v1.UserService/CreateUser": "db_users.users.write",
	})

	ctx := ctxWithSubject("user-123")
	out, err := mw(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/users.v1.UserService/CreateUser"},
		okHandler)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out != "ok" {
		t.Errorf("handler should have run; got %v", out)
	}
}

// Fail-closed: si el resolver devuelve error (Redis/gRPC caído),
// el interceptor responde 503 Unavailable — NO autoriza a ciegas.
func TestPermmw_FailsClosed_OnResolverError(t *testing.T) {
	res := &resolverMock{err: errors.New("redis: connection refused")}
	mw := UnaryServerInterceptor(res, map[string]string{
		"/users.v1.UserService/CreateUser": "db_users.users.write",
	})

	ctx := ctxWithSubject("user-123")
	_, err := mw(ctx, nil,
		&grpc.UnaryServerInfo{FullMethod: "/users.v1.UserService/CreateUser"},
		okHandler)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if status.Code(err) != codes.Unavailable {
		t.Errorf("expected Unavailable (fail-closed), got %v", status.Code(err))
	}
}

// Sin claims en el contexto → Unauthenticated. Solo puede pasar si jwtmw
// está mal cableado o falta su skip-list — defense in depth.
func TestPermmw_Unauthenticated_WhenNoClaims(t *testing.T) {
	res := &resolverMock{allow: true}
	mw := UnaryServerInterceptor(res, map[string]string{
		"/users.v1.UserService/CreateUser": "db_users.users.write",
	})

	// Contexto vacío (sin jwtmw.Claims).
	_, err := mw(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/users.v1.UserService/CreateUser"},
		okHandler)
	if err == nil {
		t.Fatal("expected Unauthenticated")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", status.Code(err))
	}
}

// _ ensures we keep the metadata import even if we trim some assertions
// later (defensive vs go-imports auto-pruning).
var _ = metadata.MD{}

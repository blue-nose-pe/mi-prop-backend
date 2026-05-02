// Package permmw — interceptor gRPC que valida permisos contra
// users-service en CADA request (no contra los claims del JWT).
//
// Modelo: el JWT solo prueba IDENTIDAD (firma + sub). La AUTORIZACIÓN
// (qué puede hacer este user) se consulta a users-service vía un
// Resolver. Eso garantiza que cuando un superadmin revoca un permiso,
// la próxima request del afectado falla en máx ~30 segundos (TTL del
// cache del resolver), no 15 minutos como en el modelo claims-based puro.
//
// Cómo se usa (en main.go de cada microservicio):
//
//   permResolver := permsclient.New(grpcConnUsers, redis, 30*time.Second)
//   methodToCode := map[string]string{
//       "/users.v1.UserService/CreateUser": "db_users.users.write",
//       "/users.v1.UserService/GetUser":    "db_users.users.read",
//       ...
//   }
//   grpc.NewServer(grpc.ChainUnaryInterceptor(
//       jwtmw.UnaryServerInterceptor(verifier, jwtSkip),
//       permmw.UnaryServerInterceptor(permResolver, methodToCode),
//       // ...
//   ))
//
// users-service usa este mismo interceptor pero con un Resolver "local"
// (in-process, sin gRPC) — ver users_service/cmd/server/main.go.
package permmw

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"users_service/internal/shared/jwtmw"
)

// Resolver: el chequeo concreto. La implementación canónica es
// permsclient.Client (gRPC a users-service + cache Redis). Para
// tests se inyecta un mock.
type Resolver interface {
	// Has retorna true si el user tiene el permission code. SuperAdmins
	// siempre devuelven true (lo decide el lado users-service —
	// PermissionQueries.HasPermission ya bypassa para superadmins).
	Has(ctx context.Context, userID, code string) (bool, error)
}

// UnaryServerInterceptor exige que el caller tenga el permiso mapeado
// para el método invocado.
//
// Reglas:
//   - Si el método NO está en `methodToCode`, pasa sin chequeo (rutas
//     "públicas" en cuanto a autorización; típico para Login, Refresh,
//     Logout, Health, Reflection — eso ya lo filtra jwtmw arriba).
//   - Si está en el map y el resolver devuelve error, responde 503
//     (Unavailable) — fail-closed para no autorizar a ciegas si
//     users-service o Redis están caídos.
//   - Si el resolver devuelve false, responde 403 PermissionDenied.
func UnaryServerInterceptor(resolver Resolver, methodToCode map[string]string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		code, mapped := methodToCode[info.FullMethod]
		if !mapped {
			return handler(ctx, req)
		}
		claims := jwtmw.FromContext(ctx)
		if claims == nil || claims.Subject == "" {
			return nil, status.Error(codes.Unauthenticated, "missing or invalid auth claims")
		}
		ok, err := resolver.Has(ctx, claims.Subject, code)
		if err != nil {
			// Fail-closed: si users-service no responde, NO autorizamos.
			// Mejor 503 momentáneo que dejar pasar una request sin verificar.
			return nil, status.Errorf(codes.Unavailable, "permissions service unavailable: %v", err)
		}
		if !ok {
			return nil, status.Errorf(codes.PermissionDenied,
				"missing permission %q for method %s", code, info.FullMethod)
		}
		return handler(ctx, req)
	}
}

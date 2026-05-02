package grpchandler

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

// LocalPermissionResolver implementa permmw.Resolver delegando al
// PermissionQueries en proceso (sin gRPC consigo mismo). Lo usa
// users-service para evitar el self-loop que tendría si usara
// permsclient (sería: users-service → gRPC → users-service).
//
// Otros microservicios usan permsclient.Client (Redis cache + gRPC a
// users-service). Solo users-service evita la red porque ES users-service.
type LocalPermissionResolver struct {
	q ports.PermissionQueries
}

func NewLocalPermissionResolver(q ports.PermissionQueries) *LocalPermissionResolver {
	return &LocalPermissionResolver{q: q}
}

func (r *LocalPermissionResolver) Has(ctx context.Context, userID, code string) (bool, error) {
	return r.q.HasPermission(ctx, domain.UserID(userID), code)
}

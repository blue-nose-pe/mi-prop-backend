// Package query contiene los casos de uso que SOLO LEEN (CQRS read side).
// Idempotentes, sin efectos secundarios. Pueden cachear agresivamente.
package query

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/shared/search"
)

// UserHandler implementa ports.UserQueries.
type UserHandler struct {
	users ports.UserRepository
	cache ports.UserCache
	perms ports.PermissionRepository
}

var _ ports.UserQueries = (*UserHandler)(nil)

func NewUserHandler(users ports.UserRepository, cache ports.UserCache, perms ports.PermissionRepository) *UserHandler {
	return &UserHandler{users: users, cache: cache, perms: perms}
}

// Get retorna un user por ID. Cache-first.
func (h *UserHandler) Get(ctx context.Context, id domain.UserID) (*domain.User, error) {
	if u, err := h.cache.Get(ctx, id); err == nil && u != nil {
		return u, nil
	}
	u, err := h.users.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	_ = h.cache.Set(ctx, u)
	return u, nil
}

// GetByEmail retorna un user por email. Sin cache (la cache es por ID).
func (h *UserHandler) GetByEmail(ctx context.Context, email domain.Email) (*domain.User, error) {
	email = email.Normalize()
	if err := email.Validate(); err != nil {
		return nil, err
	}
	return h.users.FindByEmail(ctx, email)
}

// Search delega al repo. Si en el futuro se necesita filtrar por tenant
// o colegio del caller, ese filtro adicional se inyecta acá antes de
// llamar al repo (forzar un grupo AND con school_id = <caller school>).
func (h *UserHandler) Search(ctx context.Context, req search.Request) (*search.Response, error) {
	return h.users.Search(ctx, req)
}

// Me devuelve el user + sus permission codes. Lo consume /api/users/me
// para que el front habilite UI según permisos. Si el user es superadmin
// devolvemos un único code "*" — el front interpreta como "todo permitido"
// sin tener que enumerar los permisos del catálogo.
func (h *UserHandler) Me(ctx context.Context, id domain.UserID) (*ports.MeOutput, error) {
	u, err := h.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if u.IsSuperadmin {
		return &ports.MeOutput{User: u, Permissions: []string{"*"}}, nil
	}
	codes, err := h.perms.FindCodesByUserID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ports.MeOutput{User: u, Permissions: codes}, nil
}

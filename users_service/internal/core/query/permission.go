package query

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

// PermissionHandler implementa ports.PermissionQueries.
//
// Reglas:
//   - SuperAdmins (users.is_superadmin=1) BYPASSAN el chequeo: cualquier
//     llamada a HasPermission devuelve true. Para listar sus permisos,
//     ListUserPermissions devuelve `["*"]` como sentinel.
//   - Para todos los demás, los codes salen del join
//     `user → user_permission_group → permission_group → permission`.
type PermissionHandler struct {
	users ports.UserRepository
	perms ports.PermissionRepository
}

var _ ports.PermissionQueries = (*PermissionHandler)(nil)

func NewPermissionHandler(users ports.UserRepository, perms ports.PermissionRepository) *PermissionHandler {
	return &PermissionHandler{users: users, perms: perms}
}

func (h *PermissionHandler) ListUserPermissions(ctx context.Context, userID domain.UserID) ([]string, error) {
	u, err := h.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.IsSuperadmin {
		return []string{"*"}, nil
	}
	return h.perms.FindCodesByUserID(ctx, userID)
}

// ListUserGroups devuelve los grupos (perfiles de acceso) del usuario.
func (h *PermissionHandler) ListUserGroups(ctx context.Context, userID domain.UserID) ([]domain.PermissionGroup, error) {
	return h.perms.ListGroupsByUser(ctx, userID)
}

func (h *PermissionHandler) HasPermission(ctx context.Context, userID domain.UserID, code string) (bool, error) {
	u, err := h.users.FindByID(ctx, userID)
	if err != nil {
		return false, err
	}
	if u.IsSuperadmin {
		return true, nil
	}
	codes, err := h.perms.FindCodesByUserID(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, c := range codes {
		if c == code {
			return true, nil
		}
	}
	return false, nil
}

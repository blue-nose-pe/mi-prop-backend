package query

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

// PermissionHandler implementa ports.PermissionQueries.
type PermissionHandler struct {
	perms ports.PermissionRepository
}

var _ ports.PermissionQueries = (*PermissionHandler)(nil)

func NewPermissionHandler(perms ports.PermissionRepository) *PermissionHandler {
	return &PermissionHandler{perms: perms}
}

func (h *PermissionHandler) ListUserPermissions(ctx context.Context, userID domain.UserID) ([]string, error) {
	return h.perms.FindCodesByUserID(ctx, userID)
}

func (h *PermissionHandler) HasPermission(ctx context.Context, userID domain.UserID, code string) (bool, error) {
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

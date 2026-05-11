package query

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/shared/apperr"
)

// PermissionGroupHandler implementa ports.PermissionGroupQueries.
type PermissionGroupHandler struct {
	perms ports.PermissionRepository
}

var _ ports.PermissionGroupQueries = (*PermissionGroupHandler)(nil)

func NewPermissionGroupHandler(perms ports.PermissionRepository) *PermissionGroupHandler {
	return &PermissionGroupHandler{perms: perms}
}

func (h *PermissionGroupHandler) Get(ctx context.Context, id uint32) (*domain.PermissionGroup, error) {
	if id == 0 {
		return nil, apperr.NewValidation("MISSING_ID", "id is required", "id")
	}
	return h.perms.FindGroupByID(ctx, id)
}

func (h *PermissionGroupHandler) List(ctx context.Context) ([]domain.PermissionGroup, error) {
	return h.perms.ListGroups(ctx)
}

func (h *PermissionGroupHandler) ListPermissions(ctx context.Context) ([]domain.Permission, error) {
	return h.perms.ListPermissions(ctx)
}

func (h *PermissionGroupHandler) ListUsersInGroup(ctx context.Context, in ports.ListUsersInGroupQueryInput) ([]domain.User, uint32, error) {
	if in.GroupID == 0 {
		return nil, 0, apperr.NewValidation("MISSING_ID", "group id is required", "group_id")
	}
	return h.perms.ListUsersInGroup(ctx, ports.ListUsersInGroupInput{
		GroupID:    in.GroupID,
		Search:     in.Search,
		Limit:      in.Limit,
		Offset:     in.Offset,
		ActiveOnly: in.ActiveOnly,
	})
}

package command

import (
	"context"
	"strings"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/shared/apperr"
)

// PermissionGroupHandler implementa ports.PermissionGroupCommands.
//
// Responsabilidad: validar input "puro" (campos requeridos, ids != 0) y
// delegar al PermissionRepository. NO conoce SQL ni transporte.
type PermissionGroupHandler struct {
	perms ports.PermissionRepository
}

var _ ports.PermissionGroupCommands = (*PermissionGroupHandler)(nil)

func NewPermissionGroupHandler(perms ports.PermissionRepository) *PermissionGroupHandler {
	return &PermissionGroupHandler{perms: perms}
}

func (h *PermissionGroupHandler) Create(ctx context.Context, in ports.CreateGroupInput) (*domain.PermissionGroup, error) {
	code := strings.TrimSpace(in.Code)
	name := strings.TrimSpace(in.Name)
	if code == "" {
		return nil, apperr.NewValidation("MISSING_CODE", "code is required", "code")
	}
	if name == "" {
		return nil, apperr.NewValidation("MISSING_NAME", "name is required", "name")
	}

	id, err := h.perms.CreateGroup(ctx, code, name, in.Description, in.PermissionIDs)
	if err != nil {
		return nil, err
	}
	// Re-leer para devolver el grupo con sus permisos ya hidratados.
	return h.perms.FindGroupByID(ctx, id)
}

func (h *PermissionGroupHandler) Update(ctx context.Context, in ports.UpdateGroupInput) (*domain.PermissionGroup, error) {
	if in.ID == 0 {
		return nil, apperr.NewValidation("MISSING_ID", "id is required", "id")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, apperr.NewValidation("MISSING_NAME", "name is required", "name")
	}
	if err := h.perms.UpdateGroup(ctx, in.ID, name, in.Description); err != nil {
		return nil, err
	}
	return h.perms.FindGroupByID(ctx, in.ID)
}

func (h *PermissionGroupHandler) Delete(ctx context.Context, id uint32) error {
	if id == 0 {
		return apperr.NewValidation("MISSING_ID", "id is required", "id")
	}
	return h.perms.DeleteGroup(ctx, id)
}

func (h *PermissionGroupHandler) AddPermission(ctx context.Context, groupID, permissionID uint32) error {
	if groupID == 0 {
		return apperr.NewValidation("MISSING_GROUP_ID", "group_id is required", "group_id")
	}
	if permissionID == 0 {
		return apperr.NewValidation("MISSING_PERMISSION_ID", "permission_id is required", "permission_id")
	}
	return h.perms.AddPermissionToGroup(ctx, groupID, permissionID)
}

func (h *PermissionGroupHandler) RemovePermission(ctx context.Context, groupID, permissionID uint32) error {
	if groupID == 0 {
		return apperr.NewValidation("MISSING_GROUP_ID", "group_id is required", "group_id")
	}
	if permissionID == 0 {
		return apperr.NewValidation("MISSING_PERMISSION_ID", "permission_id is required", "permission_id")
	}
	return h.perms.RemovePermissionFromGroup(ctx, groupID, permissionID)
}

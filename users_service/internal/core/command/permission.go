package command

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

// PermissionHandler implementa ports.PermissionCommands.
type PermissionHandler struct {
	users ports.UserRepository
	perms ports.PermissionRepository
}

var _ ports.PermissionCommands = (*PermissionHandler)(nil)

func NewPermissionHandler(users ports.UserRepository, perms ports.PermissionRepository) *PermissionHandler {
	return &PermissionHandler{users: users, perms: perms}
}

func (h *PermissionHandler) AssignGroup(ctx context.Context, userID domain.UserID, groupID uint32) error {
	if _, err := h.users.FindByID(ctx, userID); err != nil {
		return err
	}
	ok, err := h.perms.GroupExists(ctx, groupID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrPermGroupNotFound
	}
	return h.perms.AssignGroupToUser(ctx, userID, groupID)
}

func (h *PermissionHandler) RevokeGroup(ctx context.Context, userID domain.UserID, groupID uint32) error {
	return h.perms.RevokeGroupFromUser(ctx, userID, groupID)
}

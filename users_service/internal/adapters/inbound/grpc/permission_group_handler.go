package grpchandler

import (
	"context"

	"users_service/internal/core/ports"
	"users_service/internal/shared/apperr"
	pb "users_service/proto/gen"
)

// PermissionGroupHandler implementa pb.PermissionGroupServiceServer.
// Patrón idéntico a UserHandler: leer req → validar id → delegar al
// core (commands o queries) → mapear a proto. La traducción de errores
// vive en apperr.ToGRPC.
type PermissionGroupHandler struct {
	pb.UnimplementedPermissionGroupServiceServer

	cmds ports.PermissionGroupCommands
	qrys ports.PermissionGroupQueries
}

func NewPermissionGroupHandler(
	cmds ports.PermissionGroupCommands,
	qrys ports.PermissionGroupQueries,
) *PermissionGroupHandler {
	return &PermissionGroupHandler{cmds: cmds, qrys: qrys}
}

// ---------- Commands ----------

func (h *PermissionGroupHandler) CreateGroup(ctx context.Context, req *pb.CreateGroupRequest) (*pb.PermissionGroupResponse, error) {
	g, err := h.cmds.Create(ctx, ports.CreateGroupInput{
		Code:          req.GetCode(),
		Name:          req.GetName(),
		Description:   req.GetDescription(),
		PermissionIDs: req.GetPermissionIds(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.PermissionGroupResponse{Group: toProtoPermissionGroup(g)}, nil
}

func (h *PermissionGroupHandler) UpdateGroup(ctx context.Context, req *pb.UpdateGroupRequest) (*pb.PermissionGroupResponse, error) {
	if req.GetId() == 0 {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	g, err := h.cmds.Update(ctx, ports.UpdateGroupInput{
		ID:          req.GetId(),
		Name:        req.GetName(),
		Description: req.GetDescription(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.PermissionGroupResponse{Group: toProtoPermissionGroup(g)}, nil
}

func (h *PermissionGroupHandler) DeleteGroup(ctx context.Context, req *pb.DeleteGroupRequest) (*pb.EmptyResponse, error) {
	if req.GetId() == 0 {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	if err := h.cmds.Delete(ctx, req.GetId()); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *PermissionGroupHandler) AddPermission(ctx context.Context, req *pb.AddPermissionRequest) (*pb.EmptyResponse, error) {
	if req.GetGroupId() == 0 {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_GROUP_ID", "group_id is required", "group_id"))
	}
	if req.GetPermissionId() == 0 {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_PERMISSION_ID", "permission_id is required", "permission_id"))
	}
	if err := h.cmds.AddPermission(ctx, req.GetGroupId(), req.GetPermissionId()); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *PermissionGroupHandler) RemovePermission(ctx context.Context, req *pb.RemovePermissionRequest) (*pb.EmptyResponse, error) {
	if req.GetGroupId() == 0 {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_GROUP_ID", "group_id is required", "group_id"))
	}
	if req.GetPermissionId() == 0 {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_PERMISSION_ID", "permission_id is required", "permission_id"))
	}
	if err := h.cmds.RemovePermission(ctx, req.GetGroupId(), req.GetPermissionId()); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

// ---------- Queries ----------

func (h *PermissionGroupHandler) GetGroup(ctx context.Context, req *pb.GetGroupRequest) (*pb.PermissionGroupResponse, error) {
	if req.GetId() == 0 {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	g, err := h.qrys.Get(ctx, req.GetId())
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.PermissionGroupResponse{Group: toProtoPermissionGroup(g)}, nil
}

func (h *PermissionGroupHandler) ListGroups(ctx context.Context, _ *pb.EmptyRequest) (*pb.ListGroupsResponse, error) {
	groups, err := h.qrys.List(ctx)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	items := make([]*pb.PermissionGroup, 0, len(groups))
	for i := range groups {
		items = append(items, toProtoPermissionGroup(&groups[i]))
	}
	return &pb.ListGroupsResponse{Items: items}, nil
}

func (h *PermissionGroupHandler) ListPermissions(ctx context.Context, _ *pb.EmptyRequest) (*pb.ListPermissionsResponse, error) {
	perms, err := h.qrys.ListPermissions(ctx)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	items := make([]*pb.Permission, 0, len(perms))
	for i := range perms {
		items = append(items, toProtoPermission(&perms[i]))
	}
	return &pb.ListPermissionsResponse{Items: items}, nil
}

func (h *PermissionGroupHandler) ListGroupUsers(ctx context.Context, req *pb.ListGroupUsersRequest) (*pb.ListGroupUsersResponse, error) {
	users, total, err := h.qrys.ListUsersInGroup(ctx, ports.ListUsersInGroupQueryInput{
		GroupID:    req.GetGroupId(),
		Search:     req.GetSearch(),
		Limit:      req.GetLimit(),
		Offset:     req.GetOffset(),
		ActiveOnly: req.GetActiveOnly(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	items := make([]*pb.User, 0, len(users))
	for i := range users {
		items = append(items, toProtoUser(&users[i]))
	}
	return &pb.ListGroupUsersResponse{Items: items, Total: total}, nil
}

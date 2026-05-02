package grpchandler

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/shared/apperr"
	"users_service/internal/shared/search"
	pb "users_service/proto/gen"
	commonpb "users_service/proto/gen/common"
)

// UserHandler depende de los puertos INBOUND segregados por CQRS.
// No conoce mssql, redis, ni bcrypt. Tampoco hace switch por tipo de
// error: delega al traductor único apperr.ToGRPC.
type UserHandler struct {
	pb.UnimplementedUserServiceServer

	userCmds ports.UserCommands
	userQrys ports.UserQueries
	permCmds ports.PermissionCommands
	permQrys ports.PermissionQueries
}

func NewUserHandler(
	userCmds ports.UserCommands,
	userQrys ports.UserQueries,
	permCmds ports.PermissionCommands,
	permQrys ports.PermissionQueries,
) *UserHandler {
	return &UserHandler{
		userCmds: userCmds,
		userQrys: userQrys,
		permCmds: permCmds,
		permQrys: permQrys,
	}
}

// ---------- User commands ----------

func (h *UserHandler) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.UserResponse, error) {
	u, err := h.userCmds.Create(ctx, ports.CreateUserInput{
		Email:          req.GetEmail(),
		Password:       req.GetPassword(),
		FirstName:      req.GetFirstName(),
		LastName:       req.GetLastName(),
		DocumentNumber: req.GetDocumentNumber(),
		SchoolID:       req.GetSchoolId(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.UserResponse{User: toProtoUser(u)}, nil
}

func (h *UserHandler) UpdateUser(ctx context.Context, req *pb.UpdateUserRequest) (*pb.UserResponse, error) {
	u, err := h.userCmds.Update(ctx, ports.UpdateUserInput{
		ID:             domain.UserID(req.GetId()),
		FirstName:      req.GetFirstName(),
		LastName:       req.GetLastName(),
		DocumentNumber: req.GetDocumentNumber(),
		SchoolID:       req.GetSchoolId(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.UserResponse{User: toProtoUser(u)}, nil
}

func (h *UserHandler) DeactivateUser(ctx context.Context, req *pb.DeactivateUserRequest) (*pb.EmptyResponse, error) {
	if err := h.userCmds.Deactivate(ctx, domain.UserID(req.GetId())); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

// ---------- User queries ----------

func (h *UserHandler) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.UserResponse, error) {
	u, err := h.userQrys.Get(ctx, domain.UserID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.UserResponse{User: toProtoUser(u)}, nil
}

func (h *UserHandler) GetUserByEmail(ctx context.Context, req *pb.GetUserByEmailRequest) (*pb.UserResponse, error) {
	u, err := h.userQrys.GetByEmail(ctx, domain.Email(req.GetEmail()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.UserResponse{User: toProtoUser(u)}, nil
}

// SearchUsers: endpoint "estilo HubSpot". Mismo patrón para TODO listado.
func (h *UserHandler) SearchUsers(ctx context.Context, req *commonpb.SearchRequest) (*commonpb.SearchResponse, error) {
	resp, err := h.userQrys.Search(ctx, search.RequestFromProto(req))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	pbResp, err := search.ResponseToProto(resp)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return pbResp, nil
}

// ---------- Permission commands ----------

func (h *UserHandler) AssignPermissionGroup(ctx context.Context, req *pb.AssignGroupRequest) (*pb.EmptyResponse, error) {
	if err := h.permCmds.AssignGroup(ctx, domain.UserID(req.GetUserId()), req.GetPermissionGroupId()); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *UserHandler) RevokePermissionGroup(ctx context.Context, req *pb.RevokeGroupRequest) (*pb.EmptyResponse, error) {
	if err := h.permCmds.RevokeGroup(ctx, domain.UserID(req.GetUserId()), req.GetPermissionGroupId()); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

// ---------- Permission queries ----------

func (h *UserHandler) ListUserPermissions(ctx context.Context, req *pb.ListPermsRequest) (*pb.ListPermsResponse, error) {
	codes, err := h.permQrys.ListUserPermissions(ctx, domain.UserID(req.GetUserId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ListPermsResponse{Codes: codes}, nil
}

func (h *UserHandler) HasPermission(ctx context.Context, req *pb.HasPermissionRequest) (*pb.HasPermissionResponse, error) {
	ok, err := h.permQrys.HasPermission(ctx, domain.UserID(req.GetUserId()), req.GetCode())
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.HasPermissionResponse{Allowed: ok}, nil
}

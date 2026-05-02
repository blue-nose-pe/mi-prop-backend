package grpchandler

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/shared/apperr"
	pb "users_service/proto/gen"
)

// AuthHandler implementa pb.AuthServiceServer. Como cualquier handler:
// solo conoce los puertos inbound del core; ignora cómo se firman tokens
// o se persisten refreshes.
type AuthHandler struct {
	pb.UnimplementedAuthServiceServer
	cmds ports.AuthCommands
}

func NewAuthHandler(cmds ports.AuthCommands) *AuthHandler {
	return &AuthHandler{cmds: cmds}
}

func (h *AuthHandler) Authenticate(ctx context.Context, req *pb.AuthenticateRequest) (*pb.AuthenticateResponse, error) {
	out, err := h.cmds.Authenticate(ctx, ports.AuthenticateInput{
		Email:         domain.Email(req.GetEmail()),
		PlainPassword: req.GetPassword(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.AuthenticateResponse{User: toProtoUser(out.User), Permissions: out.Permissions}, nil
}

func (h *AuthHandler) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	out, err := h.cmds.Login(ctx, ports.LoginInput{
		Email:         domain.Email(req.GetEmail()),
		PlainPassword: req.GetPassword(),
		IP:            req.GetIp(),
		UserAgent:     req.GetUserAgent(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.LoginResponse{
		User:         toProtoUser(out.User),
		Permissions:  out.Permissions,
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
	}, nil
}

func (h *AuthHandler) Refresh(ctx context.Context, req *pb.RefreshRequest) (*pb.RefreshResponse, error) {
	out, err := h.cmds.Refresh(ctx, ports.RefreshInput{
		RefreshToken: req.GetRefreshToken(),
		IP:           req.GetIp(),
		UserAgent:    req.GetUserAgent(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.RefreshResponse{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
	}, nil
}

func (h *AuthHandler) Logout(ctx context.Context, req *pb.LogoutRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.Logout(ctx, ports.LogoutInput{RefreshToken: req.GetRefreshToken()}); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

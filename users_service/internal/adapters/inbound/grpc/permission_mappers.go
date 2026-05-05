package grpchandler

import (
	"users_service/internal/core/domain"
	pb "users_service/proto/gen"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// Mapping domain -> proto para PermissionGroup / Permission. Igual que
// con users: mapping puro, sin lógica de errores (eso lo hace apperr.ToGRPC).

func toProtoPermission(p *domain.Permission) *pb.Permission {
	if p == nil {
		return nil
	}
	return &pb.Permission{
		Id:          p.ID,
		Code:        p.Code,
		Scope:       p.Scope,
		Name:        p.Name,
		Description: p.Description,
	}
}

func toProtoPermissionGroup(g *domain.PermissionGroup) *pb.PermissionGroup {
	if g == nil {
		return nil
	}
	out := &pb.PermissionGroup{
		Id:          g.ID,
		Code:        g.Code,
		Name:        g.Name,
		Description: g.Description,
		Active:      g.Active,
		CreatedAt:   timestamppb.New(g.CreatedAt),
		UpdatedAt:   tsOrNil(g.UpdatedAt),
	}
	if len(g.Permissions) > 0 {
		out.Permissions = make([]*pb.Permission, 0, len(g.Permissions))
		for i := range g.Permissions {
			out.Permissions = append(out.Permissions, toProtoPermission(&g.Permissions[i]))
		}
	}
	return out
}

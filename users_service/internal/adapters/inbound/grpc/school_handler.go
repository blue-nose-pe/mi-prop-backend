package grpchandler

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/shared/apperr"
	pb "users_service/proto/gen"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// SchoolHandler implementa pb.SchoolServiceServer. Lectura de schools.
type SchoolHandler struct {
	pb.UnimplementedSchoolServiceServer
	repo ports.SchoolRepository
}

func NewSchoolHandler(repo ports.SchoolRepository) *SchoolHandler {
	return &SchoolHandler{repo: repo}
}

func (h *SchoolHandler) GetSchool(ctx context.Context, req *pb.GetSchoolRequest) (*pb.SchoolResponse, error) {
	if req.GetId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	s, err := h.repo.FindByID(ctx, domain.SchoolID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.SchoolResponse{School: schoolToProto(s)}, nil
}

func schoolToProto(s *domain.School) *pb.School {
	if s == nil {
		return nil
	}
	out := &pb.School{
		Id:              string(s.ID),
		UserId:          string(s.UserID),
		Name:            s.Name,
		Active:          s.Active,
		HubspotRecordId: s.HubspotRecordID,
		CreatedAt:       timestamppb.New(s.CreatedAt),
	}
	if s.UpdatedAt != nil {
		out.UpdatedAt = timestamppb.New(*s.UpdatedAt)
	}
	return out
}

package grpchandler

import (
	"context"
	"time"

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

func (h *SchoolHandler) CreateSchool(ctx context.Context, req *pb.CreateSchoolRequest) (*pb.SchoolResponse, error) {
	if req.GetName() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_NAME", "name is required", "name"))
	}
	if req.GetUserId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_USER_ID", "user_id (coordinador owner) is required", "user_id"))
	}
	if cat := req.GetCategory(); cat != "" && !isValidSchoolCategory(cat) {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("INVALID_CATEGORY", "category must be one of: A+, A, B, C, D", "category"))
	}
	s := &domain.School{
		Name:            req.GetName(),
		UserID:          domain.UserID(req.GetUserId()),
		City:            req.GetCity(),
		Category:        req.GetCategory(),
		Active:          true,
		HubspotRecordID: req.GetHubspotRecordId(),
		CreatedAt:       time.Now(),
	}
	id, err := h.repo.Create(ctx, s)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	s.ID = id
	return &pb.SchoolResponse{School: schoolToProto(s)}, nil
}

func (h *SchoolHandler) UpdateSchool(ctx context.Context, req *pb.UpdateSchoolRequest) (*pb.SchoolResponse, error) {
	if req.GetId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	if cat := req.GetCategory(); cat != "" && cat != "-" && !isValidSchoolCategory(cat) {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("INVALID_CATEGORY", "category must be one of: A+, A, B, C, D", "category"))
	}
	s := &domain.School{
		ID:              domain.SchoolID(req.GetId()),
		Name:            req.GetName(),
		UserID:          domain.UserID(req.GetUserId()),
		HubspotRecordID: req.GetHubspotRecordId(),
		City:            req.GetCity(),
		Category:        req.GetCategory(),
	}
	if err := h.repo.Update(ctx, s); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	updated, err := h.repo.FindByID(ctx, s.ID)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.SchoolResponse{School: schoolToProto(updated)}, nil
}

func (h *SchoolHandler) ListSchools(ctx context.Context, req *pb.ListSchoolsRequest) (*pb.ListSchoolsResponse, error) {
	items, total, err := h.repo.List(ctx, ports.ListSchoolsInput{
		Search:     req.GetSearch(),
		Limit:      req.GetLimit(),
		Offset:     req.GetOffset(),
		ActiveOnly: req.GetActiveOnly(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := make([]*pb.School, 0, len(items))
	for i := range items {
		out = append(out, schoolToProto(&items[i]))
	}
	return &pb.ListSchoolsResponse{Items: out, Total: total}, nil
}

func schoolToProto(s *domain.School) *pb.School {
	if s == nil {
		return nil
	}
	out := &pb.School{
		Id:              string(s.ID),
		UserId:          string(s.UserID),
		Name:            s.Name,
		City:            s.City,
		Category:        s.Category,
		Active:          s.Active,
		HubspotRecordId: s.HubspotRecordID,
		CreatedAt:       timestamppb.New(s.CreatedAt),
	}
	if s.UpdatedAt != nil {
		out.UpdatedAt = timestamppb.New(*s.UpdatedAt)
	}
	return out
}

// isValidSchoolCategory acepta los valores que el constraint check de la
// migracion 018 permite. Sincronizar si cambian alli.
func isValidSchoolCategory(c string) bool {
	switch c {
	case "A+", "A", "B", "C", "D":
		return true
	}
	return false
}

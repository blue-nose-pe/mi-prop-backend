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

// SchoolHandler implementa pb.SchoolServiceServer. Lectura de schools +
// assignment de asesores via AssignmentRepository (SCD-2).
type SchoolHandler struct {
	pb.UnimplementedSchoolServiceServer
	repo        ports.SchoolRepository
	assignments ports.AssignmentRepository
}

func NewSchoolHandler(repo ports.SchoolRepository, assignments ports.AssignmentRepository) *SchoolHandler {
	return &SchoolHandler{repo: repo, assignments: assignments}
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

func (h *SchoolHandler) ListSchoolsByAsesor(ctx context.Context, req *pb.ListSchoolsByAsesorRequest) (*pb.ListSchoolsResponse, error) {
	if req.GetAsesorId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ASESOR_ID", "asesor_id is required", "asesor_id"))
	}
	items, err := h.repo.ListByAsesor(ctx, domain.UserID(req.GetAsesorId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := make([]*pb.School, 0, len(items))
	for i := range items {
		out = append(out, schoolToProto(&items[i]))
	}
	return &pb.ListSchoolsResponse{Items: out, Total: uint32(len(out))}, nil
}

// AssignAsesor: registra una asignacion SCD-2 kind=asesor_de_colegio donde
// source=asesor (user a asignar) y target=coordinador del colegio
// (school.user_id). Cierra la vigente previa si existe.
func (h *SchoolHandler) AssignAsesor(ctx context.Context, req *pb.AssignAsesorRequest) (*pb.EmptyResponse, error) {
	if req.GetSchoolId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_SCHOOL_ID", "school_id is required", "school_id"))
	}
	if req.GetUserId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_USER_ID", "user_id (asesor) is required", "user_id"))
	}
	s, err := h.repo.FindByID(ctx, domain.SchoolID(req.GetSchoolId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	if s == nil || s.UserID == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("SCHOOL_NO_COORDINATOR", "school has no coordinator user_id", "school_id"))
	}
	// source = asesor (nuevo), target = coordinador del colegio.
	// by = asesor mismo por simplicidad — el audit log de assignment recoge created_by.
	if err := h.assignments.Reassign(
		ctx,
		ports.AssignmentAsesorDeColegio,
		domain.UserID(req.GetUserId()),
		s.UserID,
		domain.UserID(req.GetUserId()),
	); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
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
		IntId:           s.IntID,
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

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

type VisitaHandler struct {
	pb.UnimplementedVisitaServiceServer
	repo ports.VisitaRepository
}

func NewVisitaHandler(repo ports.VisitaRepository) *VisitaHandler {
	return &VisitaHandler{repo: repo}
}

func (h *VisitaHandler) CreateVisita(ctx context.Context, req *pb.CreateVisitaRequest) (*pb.VisitaResponse, error) {
	if req.GetAsesorUserId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ASESOR", "asesor_user_id is required", "asesor_user_id"))
	}
	if req.GetSchoolId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_SCHOOL", "school_id is required", "school_id"))
	}
	if req.GetScheduledAt() == nil {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_SCHEDULED_AT", "scheduled_at is required", "scheduled_at"))
	}
	status := domain.VisitaStatus(req.GetStatus())
	if status == "" {
		status = domain.VisitaScheduled
	}
	if !status.IsValid() {
		return nil, apperr.ToGRPC(ctx, domain.ErrInvalidVisitaStatus)
	}
	v := &domain.Visita{
		AsesorUserID: domain.UserID(req.GetAsesorUserId()),
		SchoolID:     domain.SchoolID(req.GetSchoolId()),
		ScheduledAt:  req.GetScheduledAt().AsTime(),
		Status:       status,
		Notes:        req.GetNotes(),
	}
	if t := req.GetCompletedAt(); t != nil {
		ct := t.AsTime()
		if !ct.IsZero() {
			v.CompletedAt = &ct
		}
	}
	id, err := h.repo.Create(ctx, v)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out, err := h.repo.FindByID(ctx, id)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.VisitaResponse{Visita: visitaToProto(out)}, nil
}

func (h *VisitaHandler) UpdateVisita(ctx context.Context, req *pb.UpdateVisitaRequest) (*pb.VisitaResponse, error) {
	if req.GetId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	if s := req.GetStatus(); s != "" && !domain.VisitaStatus(s).IsValid() {
		return nil, apperr.ToGRPC(ctx, domain.ErrInvalidVisitaStatus)
	}
	v := &domain.Visita{
		ID:     domain.VisitaID(req.GetId()),
		Status: domain.VisitaStatus(req.GetStatus()),
		Notes:  req.GetNotes(),
	}
	if t := req.GetScheduledAt(); t != nil {
		v.ScheduledAt = t.AsTime()
	}
	if t := req.GetCompletedAt(); t != nil {
		ct := t.AsTime()
		v.CompletedAt = &ct
	}
	if err := h.repo.Update(ctx, v); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out, err := h.repo.FindByID(ctx, v.ID)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.VisitaResponse{Visita: visitaToProto(out)}, nil
}

func (h *VisitaHandler) GetVisita(ctx context.Context, req *pb.GetVisitaRequest) (*pb.VisitaResponse, error) {
	if req.GetId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	v, err := h.repo.FindByID(ctx, domain.VisitaID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.VisitaResponse{Visita: visitaToProto(v)}, nil
}

func (h *VisitaHandler) ListVisitas(ctx context.Context, req *pb.ListVisitasRequest) (*pb.ListVisitasResponse, error) {
	in := ports.ListVisitasInput{
		AsesorUserID: domain.UserID(req.GetAsesorUserId()),
		SchoolID:     domain.SchoolID(req.GetSchoolId()),
		Status:       domain.VisitaStatus(req.GetStatus()),
		Limit:        req.GetLimit(),
		Offset:       req.GetOffset(),
	}
	if t := req.GetFrom(); t != nil {
		ft := t.AsTime()
		if !ft.IsZero() {
			in.From = &ft
		}
	}
	if t := req.GetTo(); t != nil {
		tt := t.AsTime()
		if !tt.IsZero() {
			in.To = &tt
		}
	}
	items, total, err := h.repo.List(ctx, in)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := &pb.ListVisitasResponse{Items: make([]*pb.Visita, 0, len(items)), Total: total}
	for i := range items {
		out.Items = append(out.Items, visitaToProto(&items[i]))
	}
	return out, nil
}

func visitaToProto(v *domain.Visita) *pb.Visita {
	if v == nil {
		return nil
	}
	out := &pb.Visita{
		Id:           string(v.ID),
		AsesorUserId: string(v.AsesorUserID),
		SchoolId:     string(v.SchoolID),
		ScheduledAt:  timestamppb.New(v.ScheduledAt),
		Status:       string(v.Status),
		Notes:        v.Notes,
		CreatedAt:    timestamppb.New(v.CreatedAt),
	}
	if v.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*v.CompletedAt)
	}
	if v.UpdatedAt != nil {
		out.UpdatedAt = timestamppb.New(*v.UpdatedAt)
	}
	return out
}

// stamp evita unused import si se reordenan handlers que dejen el time
// solo en visitaToProto. Defensivo: cualquier handler que necesite
// stamping puede usarlo.
var _ = time.Now

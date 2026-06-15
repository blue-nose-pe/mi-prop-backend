package grpchandler

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/shared/apperr"
	pb "users_service/proto/gen"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// LeadHandler implementa pb.LeadServiceServer. Captacion de leads de la
// landing publica "Preparate" (simulacro masivo). CreateLead es publico
// (el gateway lo expone en /api/public/leads sin auth); ListLeads alimenta
// la reporteria del masivo.
type LeadHandler struct {
	pb.UnimplementedLeadServiceServer
	repo ports.LeadRepository
}

func NewLeadHandler(repo ports.LeadRepository) *LeadHandler {
	return &LeadHandler{repo: repo}
}

func (h *LeadHandler) CreateLead(ctx context.Context, req *pb.CreateLeadRequest) (*pb.LeadResponse, error) {
	if req.GetFirstName() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_FIRST_NAME", "first_name is required", "first_name"))
	}
	if req.GetLastName() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_LAST_NAME", "last_name is required", "last_name"))
	}
	if req.GetEmail() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_EMAIL", "email is required", "email"))
	}
	l := &domain.Lead{
		FirstName:      req.GetFirstName(),
		LastName:       req.GetLastName(),
		DNI:            req.GetDni(),
		Phone:          req.GetPhone(),
		Email:          req.GetEmail(),
		GraduationYear: req.GetGraduationYear(),
		SchoolText:     req.GetSchoolText(),
		Origen:         req.GetOrigen(),
		KeyCode:        req.GetKeyCode(),
		TermsAccepted:  req.GetTermsAccepted(),
		DataProcessing: req.GetDataProcessing(),
	}
	id, err := h.repo.Create(ctx, l)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	saved, err := h.repo.FindByID(ctx, id)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.LeadResponse{Lead: leadToProto(saved)}, nil
}

func (h *LeadHandler) ListLeads(ctx context.Context, req *pb.ListLeadsRequest) (*pb.ListLeadsResponse, error) {
	items, total, err := h.repo.List(ctx, ports.ListLeadsInput{
		Search:  req.GetSearch(),
		KeyCode: req.GetKeyCode(),
		Limit:   req.GetLimit(),
		Offset:  req.GetOffset(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := make([]*pb.Lead, 0, len(items))
	for i := range items {
		out = append(out, leadToProto(&items[i]))
	}
	return &pb.ListLeadsResponse{Items: out, Total: total}, nil
}

func leadToProto(l *domain.Lead) *pb.Lead {
	if l == nil {
		return nil
	}
	return &pb.Lead{
		Id:              string(l.ID),
		FirstName:       l.FirstName,
		LastName:        l.LastName,
		Dni:             l.DNI,
		Phone:           l.Phone,
		Email:           l.Email,
		GraduationYear:  l.GraduationYear,
		SchoolText:      l.SchoolText,
		Origen:          l.Origen,
		KeyCode:         l.KeyCode,
		TermsAccepted:   l.TermsAccepted,
		DataProcessing:  l.DataProcessing,
		HubspotRecordId: l.HubspotRecordID,
		CreatedAt:       timestamppb.New(l.CreatedAt),
	}
}

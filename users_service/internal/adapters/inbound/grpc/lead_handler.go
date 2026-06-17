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

// GetLeadsByIDs resuelve datos de varios leads por id. Lo usa el gateway en
// el envio masivo del panel: el front manda los ids seleccionados y aqui se
// resuelven email/nombre/dni/phone para mandarles el acceso.
func (h *LeadHandler) GetLeadsByIDs(ctx context.Context, req *pb.GetLeadsByIDsRequest) (*pb.ListLeadsResponse, error) {
	ids := make([]domain.LeadID, 0, len(req.GetIds()))
	for _, s := range req.GetIds() {
		if s != "" {
			ids = append(ids, domain.LeadID(s))
		}
	}
	items, err := h.repo.ListByIDs(ctx, ids)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := make([]*pb.Lead, 0, len(items))
	for i := range items {
		out = append(out, leadToProto(&items[i]))
	}
	return &pb.ListLeadsResponse{Items: out, Total: uint32(len(out))}, nil
}

// MarkLeadAccessSent marca que al lead se le envio el correo de acceso con la
// key indicada (anti-doble-envio + estado "enviado" en el panel).
func (h *LeadHandler) MarkLeadAccessSent(ctx context.Context, req *pb.MarkLeadAccessSentRequest) (*pb.LeadResponse, error) {
	if req.GetLeadId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_LEAD_ID", "lead_id is required", "lead_id"))
	}
	if err := h.repo.MarkAccessSent(ctx, domain.LeadID(req.GetLeadId()), req.GetKeyCode()); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	saved, err := h.repo.FindByID(ctx, domain.LeadID(req.GetLeadId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.LeadResponse{Lead: leadToProto(saved)}, nil
}

func leadToProto(l *domain.Lead) *pb.Lead {
	if l == nil {
		return nil
	}
	p := &pb.Lead{
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
		AccesoKeyCode:   l.AccesoKeyCode,
	}
	if l.AccesoEnviadoAt != nil {
		p.AccesoEnviadoAt = timestamppb.New(*l.AccesoEnviadoAt)
	}
	return p
}

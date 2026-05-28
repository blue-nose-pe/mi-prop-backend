package grpchandler

import (
	"context"
	"time"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
	"hubspot_service/internal/shared/apperr"

	pb "hubspot_service/proto/gen"
)

type HubspotHandler struct {
	pb.UnimplementedHubspotServiceServer
	cmds  ports.SyncCommands
	qrys  ports.SyncQueries
	admin ports.AdminCommands
}

func NewHubspotHandler(c ports.SyncCommands, q ports.SyncQueries, a ports.AdminCommands) *HubspotHandler {
	return &HubspotHandler{cmds: c, qrys: q, admin: a}
}

func (h *HubspotHandler) UpsertContact(ctx context.Context, req *pb.UpsertContactRequest) (*pb.UpsertContactResponse, error) {
	c := domain.Contact{
		DNI:        req.GetDni(),
		Email:      req.GetEmail(),
		FirstName:  req.GetFirstName(),
		LastName:   req.GetLastName(),
		Phone:      req.GetPhone(),
		SchoolName: req.GetSchoolName(),
		Grade:      req.GetGrade(),
		UserID:     domain.UserID(req.GetUserId()),
		Extra:      req.GetExtra(),
	}
	id, err := h.cmds.UpsertContact(ctx, c)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.UpsertContactResponse{RecordId: string(id)}, nil
}

func (h *HubspotHandler) GetContactByDNI(ctx context.Context, req *pb.GetContactByDNIRequest) (*pb.UpsertContactResponse, error) {
	id, err := h.qrys.GetContactByDNI(ctx, req.GetDni())
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.UpsertContactResponse{RecordId: string(id)}, nil
}

func (h *HubspotHandler) SendOTP(ctx context.Context, req *pb.SendOTPRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.SendOTP(ctx, ports.SendOTPInput{
		Email:          req.GetEmail(),
		OTP:            req.GetOtp(),
		FirstName:      req.GetFirstName(),
		LastName:       req.GetLastName(),
		DocumentNumber: req.GetDocumentNumber(),
		Phone:          req.GetPhone(),
		SchoolID:       req.GetSchoolId(),
		SchoolIntID:    req.GetSchoolIntId(),
		SchoolRecordID: req.GetSchoolRecordId(),
	}); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *HubspotHandler) SyncStudentContact(ctx context.Context, req *pb.SyncStudentContactRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.SyncStudentContact(ctx, ports.SyncStudentContactInput{
		Email:          req.GetEmail(),
		FirstName:      req.GetFirstName(),
		LastName:       req.GetLastName(),
		DocumentNumber: req.GetDocumentNumber(),
		Phone:          req.GetPhone(),
		SchoolIntID:    req.GetSchoolIntId(),
		SchoolRecordID: req.GetSchoolRecordId(),
	}); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *HubspotHandler) SyncKey(ctx context.Context, req *pb.SyncKeyRequest) (*pb.EmptyResponse, error) {
	validFrom, _ := time.Parse(time.RFC3339, req.GetValidFrom())
	validTo, _ := time.Parse(time.RFC3339, req.GetValidTo())
	if err := h.cmds.SyncKey(ctx, domain.KeyPayload{
		Code:           req.GetCode(),
		ExamTypeCode:   domain.ExamTypeCode(req.GetExamTypeCode()),
		AsesorUserID:   domain.UserID(req.GetAsesorUserId()),
		SchoolID:       domain.SchoolID(req.GetSchoolId()),
		SchoolName:     req.GetSchoolName(),
		Grade:          req.GetGrade(),
		Section:        req.GetSection(),
		ValidFrom:      validFrom,
		ValidTo:        validTo,
		MaxUses:        req.GetMaxUses(),
		AsesorRecordID: domain.RecordID(req.GetAsesorRecordId()),
		SchoolRecordID: domain.RecordID(req.GetSchoolRecordId()),
		AsesorEmail:    req.GetAsesorEmail(),
		SchoolIntID:    req.GetSchoolIntId(),
		AsesorIntID:    req.GetAsesorIntId(),
	}); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *HubspotHandler) SyncExamResult(ctx context.Context, req *pb.SyncExamResultRequest) (*pb.EmptyResponse, error) {
	t, _ := time.Parse(time.RFC3339, req.GetSubmittedAt())
	r := domain.ExamResult{
		DNI:           req.GetDni(),
		ExamTypeCode:  domain.ExamTypeCode(req.GetExamTypeCode()),
		Score:         req.GetScore(),
		MaxScore:      req.GetMaxScore(),
		AttemptID:     domain.AttemptID(req.GetAttemptId()),
		SubmittedAt:   t,
		ContactRecord: domain.RecordID(req.GetContactRecordId()),
	}
	if err := h.cmds.EnqueueExamResult(ctx, r); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *HubspotHandler) UpsertSchool(ctx context.Context, req *pb.UpsertSchoolRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.EnqueueColegio(ctx, domain.ColegioPayload{
		SchoolID:       domain.SchoolID(req.GetSchoolId()),
		Email:          req.GetEmail(),
		Nombre:         req.GetNombre(),
		PersonalACargo: req.GetPersonalACargo(),
	}); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *HubspotHandler) UpsertAsesor(ctx context.Context, req *pb.UpsertAsesorRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.EnqueueAsesor(ctx, domain.AsesorPayload{
		AsesorUserID: domain.UserID(req.GetAsesorUserId()),
		Email:        req.GetEmail(),
		Nombre:       req.GetNombre(),
		Bio:          req.GetBio(),
	}); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *HubspotHandler) GetFailedSyncs(ctx context.Context, req *pb.GetFailedSyncsRequest) (*pb.GetFailedSyncsResponse, error) {
	items, err := h.qrys.ListFailed(ctx, req.GetLimit(), req.GetOffset())
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := &pb.GetFailedSyncsResponse{Items: make([]*pb.FailedSync, 0, len(items))}
	for _, it := range items {
		out.Items = append(out.Items, &pb.FailedSync{
			Queue:        it.Queue,
			JobId:        it.JobID,
			FailedReason: it.FailedReason,
			Payload:      it.Payload,
			FailedAt:     it.FailedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

func (h *HubspotHandler) RetryFailed(ctx context.Context, req *pb.RetryFailedRequest) (*pb.RetryFailedResponse, error) {
	if req.GetQueue() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_QUEUE", "queue is required", "queue"))
	}
	if req.GetJobId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_JOB_ID", "job_id is required", "job_id"))
	}
	if err := h.admin.RetryFailed(ctx, req.GetQueue(), req.GetJobId()); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.RetryFailedResponse{Ok: true}, nil
}

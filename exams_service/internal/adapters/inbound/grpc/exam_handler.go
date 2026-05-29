package grpchandler

import (
	"context"
	"time"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
	"exams_service/internal/shared/apperr"
	"exams_service/internal/shared/search"
	commonpb "exams_service/proto/gen/common"

	pb "exams_service/proto/gen"
)

type ExamHandler struct {
	pb.UnimplementedExamServiceServer
	cmds ports.ExamCommands
	qrys ports.ExamQueries
}

func NewExamHandler(cmds ports.ExamCommands, qrys ports.ExamQueries) *ExamHandler {
	return &ExamHandler{cmds: cmds, qrys: qrys}
}

func (h *ExamHandler) CreateExam(ctx context.Context, req *pb.CreateExamRequest) (*pb.ExamResponse, error) {
	e, err := h.cmds.Create(ctx, ports.CreateExamInput{
		ExamTypeCode:    req.GetExamTypeCode(),
		SchoolID:        domain.SchoolID(req.GetSchoolId()),
		Code:            req.GetCode(),
		Name:            req.GetName(),
		StartAt:         req.GetStartAt().AsTime(),
		EndAt:           req.GetEndAt().AsTime(),
		MaxParticipants: req.GetMaxParticipants(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ExamResponse{Exam: toProtoExam(e)}, nil
}

func (h *ExamHandler) UpdateExam(ctx context.Context, req *pb.UpdateExamRequest) (*pb.ExamResponse, error) {
	// req.StartAt/EndAt son *timestamppb.Timestamp; nil viene del gateway
	// cuando el JSON no incluye el campo (PATCH parcial). `AsTime()` sobre
	// nil retorna epoch (1970-01-01), NO time.Time zero, asi que el handler
	// downstream lo trataba como "valor explicito = 1970" y sobreescribia
	// las fechas reales del exam → ErrInvalidDateRange en updates de solo
	// code/name. Mapeamos nil → time.Time{} (zero) para que el handler vea
	// "no-op" via IsZero(). Bug detectado al crear nueva version del exam
	// desde la UI.
	var startAt, endAt time.Time
	if req.GetStartAt() != nil {
		startAt = req.GetStartAt().AsTime()
	}
	if req.GetEndAt() != nil {
		endAt = req.GetEndAt().AsTime()
	}
	e, err := h.cmds.Update(ctx, ports.UpdateExamInput{
		ID:              domain.ExamID(req.GetId()),
		Code:            req.GetCode(),
		Name:            req.GetName(),
		StartAt:         startAt,
		EndAt:           endAt,
		MaxParticipants: req.GetMaxParticipants(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ExamResponse{Exam: toProtoExam(e)}, nil
}

func (h *ExamHandler) GetExam(ctx context.Context, req *pb.GetExamRequest) (*pb.ExamResponse, error) {
	e, err := h.qrys.Get(ctx, domain.ExamID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ExamResponse{Exam: toProtoExam(e)}, nil
}

func (h *ExamHandler) PublishExam(ctx context.Context, req *pb.PublishExamRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.Publish(ctx, domain.ExamID(req.GetId())); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *ExamHandler) DeactivateExam(ctx context.Context, req *pb.DeactivateExamRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.Deactivate(ctx, domain.ExamID(req.GetId())); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *ExamHandler) ReactivateExam(ctx context.Context, req *pb.ReactivateExamRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.Reactivate(ctx, domain.ExamID(req.GetId())); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *ExamHandler) CloneExam(ctx context.Context, req *pb.CloneExamRequest) (*pb.ExamResponse, error) {
	e, err := h.cmds.Clone(ctx, domain.ExamID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ExamResponse{Exam: toProtoExam(e)}, nil
}

func (h *ExamHandler) SearchExams(ctx context.Context, req *commonpb.SearchRequest) (*commonpb.SearchResponse, error) {
	resp, err := h.qrys.Search(ctx, search.RequestFromProto(req))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	pbResp, err := search.ResponseToProto(resp)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return pbResp, nil
}

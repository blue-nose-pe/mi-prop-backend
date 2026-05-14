package grpchandler

import (
	"context"

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
	e, err := h.cmds.Update(ctx, ports.UpdateExamInput{
		ID:              domain.ExamID(req.GetId()),
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

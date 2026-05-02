package grpchandler

import (
	"context"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
	"exams_service/internal/shared/apperr"

	pb "exams_service/proto/gen"
)

type ExamQuestionHandler struct {
	pb.UnimplementedExamQuestionServiceServer
	cmds ports.ExamQuestionCommands
	qrys ports.ExamQuestionQueries
}

func NewExamQuestionHandler(cmds ports.ExamQuestionCommands, qrys ports.ExamQuestionQueries) *ExamQuestionHandler {
	return &ExamQuestionHandler{cmds: cmds, qrys: qrys}
}

func (h *ExamQuestionHandler) AddQuestion(ctx context.Context, req *pb.AddExamQuestionRequest) (*pb.EmptyResponse, error) {
	err := h.cmds.Add(ctx, ports.AddExamQuestionInput{
		ExamID:     domain.ExamID(req.GetExamId()),
		QuestionID: domain.QuestionID(req.GetQuestionId()),
		Points:     req.GetPoints(),
		SortOrder:  req.GetSortOrder(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *ExamQuestionHandler) RemoveQuestion(ctx context.Context, req *pb.RemoveExamQuestionRequest) (*pb.EmptyResponse, error) {
	err := h.cmds.Remove(ctx, domain.ExamID(req.GetExamId()), domain.QuestionID(req.GetQuestionId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *ExamQuestionHandler) Reorder(ctx context.Context, req *pb.ReorderRequest) (*pb.EmptyResponse, error) {
	ids := make([]domain.QuestionID, 0, len(req.GetQuestionIds()))
	for _, id := range req.GetQuestionIds() {
		ids = append(ids, domain.QuestionID(id))
	}
	if err := h.cmds.Reorder(ctx, domain.ExamID(req.GetExamId()), ids); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *ExamQuestionHandler) ListByExam(ctx context.Context, req *pb.ListByExamRequest) (*pb.ListByExamResponse, error) {
	items, err := h.qrys.List(ctx, domain.ExamID(req.GetExamId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := &pb.ListByExamResponse{Items: make([]*pb.ExamQuestion, 0, len(items))}
	for _, eq := range items {
		out.Items = append(out.Items, &pb.ExamQuestion{
			ExamId:     string(eq.ExamID),
			QuestionId: string(eq.QuestionID),
			Points:     eq.Points,
			SortOrder:  eq.SortOrder,
		})
	}
	return out, nil
}

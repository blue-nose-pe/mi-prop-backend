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

type QuestionHandler struct {
	pb.UnimplementedQuestionServiceServer
	cmds ports.QuestionCommands
	qrys ports.QuestionQueries
}

func NewQuestionHandler(cmds ports.QuestionCommands, qrys ports.QuestionQueries) *QuestionHandler {
	return &QuestionHandler{cmds: cmds, qrys: qrys}
}

func (h *QuestionHandler) CreateQuestion(ctx context.Context, req *pb.CreateQuestionRequest) (*pb.QuestionResponse, error) {
	q, err := h.cmds.Create(ctx, ports.CreateQuestionInput{
		Text:     req.GetText(),
		Category: req.GetCategory(),
		Kind:     req.GetKind(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.QuestionResponse{Question: toProtoQuestion(q)}, nil
}

func (h *QuestionHandler) UpdateQuestion(ctx context.Context, req *pb.UpdateQuestionRequest) (*pb.QuestionResponse, error) {
	q, err := h.cmds.Update(ctx, ports.UpdateQuestionInput{
		ID:       domain.QuestionID(req.GetId()),
		Text:     req.GetText(),
		Category: req.GetCategory(),
		Kind:     req.GetKind(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.QuestionResponse{Question: toProtoQuestion(q)}, nil
}

func (h *QuestionHandler) DeactivateQuestion(ctx context.Context, req *pb.DeactivateQuestionRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.Deactivate(ctx, domain.QuestionID(req.GetId())); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *QuestionHandler) GetQuestion(ctx context.Context, req *pb.GetQuestionRequest) (*pb.QuestionResponse, error) {
	q, err := h.qrys.Get(ctx, domain.QuestionID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.QuestionResponse{Question: toProtoQuestion(q)}, nil
}

func (h *QuestionHandler) ListOptions(ctx context.Context, req *pb.ListOptionsRequest) (*pb.ListOptionsResponse, error) {
	opts, err := h.qrys.ListOptions(ctx, domain.QuestionID(req.GetQuestionId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := &pb.ListOptionsResponse{Options: make([]*pb.QuestionOption, 0, len(opts))}
	for i := range opts {
		out.Options = append(out.Options, toProtoOption(&opts[i]))
	}
	return out, nil
}

func (h *QuestionHandler) AddOption(ctx context.Context, req *pb.AddOptionRequest) (*pb.OptionResponse, error) {
	o, err := h.cmds.AddOption(ctx, ports.AddOptionInput{
		QuestionID: domain.QuestionID(req.GetQuestionId()),
		Text:       req.GetText(),
		IsCorrect:  req.GetIsCorrect(),
		SortOrder:  req.GetSortOrder(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.OptionResponse{Option: toProtoOption(o)}, nil
}

func (h *QuestionHandler) UpdateOption(ctx context.Context, req *pb.UpdateOptionRequest) (*pb.EmptyResponse, error) {
	err := h.cmds.UpdateOption(ctx, ports.UpdateOptionInput{
		ID:        domain.OptionID(req.GetId()),
		Text:      req.GetText(),
		IsCorrect: req.GetIsCorrect(),
		SortOrder: req.GetSortOrder(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *QuestionHandler) RemoveOption(ctx context.Context, req *pb.RemoveOptionRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.RemoveOption(ctx, domain.OptionID(req.GetId())); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *QuestionHandler) SearchQuestions(ctx context.Context, req *commonpb.SearchRequest) (*commonpb.SearchResponse, error) {
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

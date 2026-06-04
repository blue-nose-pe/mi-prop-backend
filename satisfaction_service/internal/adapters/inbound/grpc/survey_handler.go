package grpchandler

import (
	"context"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
	"satisfaction_service/internal/shared/apperr"
	"satisfaction_service/internal/shared/search"
	pb "satisfaction_service/proto/gen"
	commonpb "satisfaction_service/proto/gen/common"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type SurveyHandler struct {
	pb.UnimplementedSurveyServiceServer
	cmds ports.SurveyCommands
	qrys ports.SurveyQueries
}

func NewSurveyHandler(cmds ports.SurveyCommands, qrys ports.SurveyQueries) *SurveyHandler {
	return &SurveyHandler{cmds: cmds, qrys: qrys}
}

func (h *SurveyHandler) CreateSurvey(ctx context.Context, req *pb.CreateSurveyRequest) (*pb.SurveyResponse, error) {
	s, err := h.cmds.Create(ctx, ports.CreateSurveyInput{
		Code:        req.GetCode(),
		Title:       req.GetTitle(),
		Description: req.GetDescription(),
		TargetRole:  req.GetTargetRole(),
		Trigger:     req.GetTriggerKind(),
		KeyID:       req.GetKeyId(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.SurveyResponse{Survey: toProtoSurvey(s)}, nil
}

func (h *SurveyHandler) UpdateSurvey(ctx context.Context, req *pb.UpdateSurveyRequest) (*pb.SurveyResponse, error) {
	if req.GetId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	s, err := h.cmds.Update(ctx, ports.UpdateSurveyInput{
		ID:          domain.SurveyID(req.GetId()),
		Title:       req.GetTitle(),
		Description: req.GetDescription(),
		// Cliente: appliesTo (trigger_kind) editable post-creacion.
		Trigger: req.GetTriggerKind(),
		KeyID:   req.GetKeyId(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.SurveyResponse{Survey: toProtoSurvey(s)}, nil
}

func (h *SurveyHandler) PublishSurvey(ctx context.Context, req *pb.PublishSurveyRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.Publish(ctx, domain.SurveyID(req.GetId())); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *SurveyHandler) DeactivateSurvey(ctx context.Context, req *pb.DeactivateSurveyRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.Deactivate(ctx, domain.SurveyID(req.GetId())); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *SurveyHandler) ReactivateSurvey(ctx context.Context, req *pb.ReactivateSurveyRequest) (*pb.SurveyResponse, error) {
	if req.GetId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	s, err := h.cmds.Reactivate(ctx, domain.SurveyID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.SurveyResponse{Survey: toProtoSurvey(s)}, nil
}

func (h *SurveyHandler) CloneSurvey(ctx context.Context, req *pb.CloneSurveyRequest) (*pb.SurveyResponse, error) {
	if req.GetId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	s, err := h.cmds.Clone(ctx, domain.SurveyID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.SurveyResponse{Survey: toProtoSurvey(s)}, nil
}

func (h *SurveyHandler) GetSurvey(ctx context.Context, req *pb.GetSurveyRequest) (*pb.SurveyResponse, error) {
	if req.GetId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	s, err := h.qrys.Get(ctx, domain.SurveyID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.SurveyResponse{Survey: toProtoSurvey(s)}, nil
}

func (h *SurveyHandler) GetByCode(ctx context.Context, req *pb.GetByCodeRequest) (*pb.SurveyResponse, error) {
	s, err := h.qrys.GetByCode(ctx, req.GetCode(), req.GetVersion())
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.SurveyResponse{Survey: toProtoSurvey(s)}, nil
}

func (h *SurveyHandler) ListQuestions(ctx context.Context, req *pb.ListQuestionsRequest) (*pb.ListQuestionsResponse, error) {
	items, err := h.qrys.ListQuestions(ctx, domain.SurveyID(req.GetSurveyId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := &pb.ListQuestionsResponse{Items: make([]*pb.Question, 0, len(items))}
	for i := range items {
		out.Items = append(out.Items, toProtoQuestion(&items[i]))
	}
	return out, nil
}

func (h *SurveyHandler) AddQuestion(ctx context.Context, req *pb.AddQuestionRequest) (*pb.QuestionResponse, error) {
	q, err := h.cmds.AddQuestion(ctx, ports.AddQuestionInput{
		SurveyID:    domain.SurveyID(req.GetSurveyId()),
		Text:        req.GetText(),
		Kind:        domain.QuestionKind(req.GetKind()),
		SortOrder:   req.GetSortOrder(),
		OptionsJSON: req.GetOptionsJson(),
		Required:    req.GetRequired(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.QuestionResponse{Question: toProtoQuestion(q)}, nil
}

func (h *SurveyHandler) UpdateQuestion(ctx context.Context, req *pb.UpdateQuestionRequest) (*pb.EmptyResponse, error) {
	err := h.cmds.UpdateQuestion(ctx, ports.UpdateQuestionInput{
		ID:          domain.QuestionID(req.GetId()),
		Text:        req.GetText(),
		SortOrder:   req.GetSortOrder(),
		OptionsJSON: req.GetOptionsJson(),
		Required:    req.GetRequired(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *SurveyHandler) RemoveQuestion(ctx context.Context, req *pb.RemoveQuestionRequest) (*pb.EmptyResponse, error) {
	if err := h.cmds.RemoveQuestion(ctx, domain.QuestionID(req.GetId())); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

func (h *SurveyHandler) Search(ctx context.Context, req *commonpb.SearchRequest) (*commonpb.SearchResponse, error) {
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

// ----- mappers -----

func toProtoSurvey(s *domain.Survey) *pb.Survey {
	if s == nil {
		return nil
	}
	out := &pb.Survey{
		Id:          string(s.ID),
		Code:        s.Code,
		Title:       s.Title,
		Description: s.Description,
		TargetRole:  s.TargetRole,
		TriggerKind: s.Trigger,
		KeyId:       s.KeyID,
		Version:     s.Version,
		Published:   s.Published,
		Active:      s.Active,
		CreatedAt:   timestamppb.New(s.CreatedAt),
	}
	if s.UpdatedAt != nil {
		out.UpdatedAt = timestamppb.New(*s.UpdatedAt)
	}
	return out
}

func toProtoQuestion(q *domain.Question) *pb.Question {
	if q == nil {
		return nil
	}
	return &pb.Question{
		Id:          string(q.ID),
		SurveyId:    string(q.SurveyID),
		Text:        q.Text,
		Kind:        string(q.Kind),
		SortOrder:   q.SortOrder,
		OptionsJson: q.OptionsJSON,
		Required:    q.Required,
	}
}

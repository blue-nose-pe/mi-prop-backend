package grpchandler

import (
	"context"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
	"satisfaction_service/internal/shared/apperr"
	pb "satisfaction_service/proto/gen"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type ResponseHandler struct {
	pb.UnimplementedResponseServiceServer
	cmds ports.ResponseCommands
	qrys ports.ResponseQueries
}

func NewResponseHandler(cmds ports.ResponseCommands, qrys ports.ResponseQueries) *ResponseHandler {
	return &ResponseHandler{cmds: cmds, qrys: qrys}
}

func (h *ResponseHandler) Submit(ctx context.Context, req *pb.SubmitResponseRequest) (*pb.ResponseObjectResponse, error) {
	answers := make([]ports.SubmittedAnswer, 0, len(req.GetAnswers()))
	for _, a := range req.GetAnswers() {
		var num *int32
		if a.GetHasNumber() {
			v := a.GetValueNumber()
			num = &v
		}
		answers = append(answers, ports.SubmittedAnswer{
			QuestionID:  domain.QuestionID(a.GetQuestionId()),
			ValueText:   a.GetValueText(),
			ValueNumber: num,
		})
	}
	r, err := h.cmds.Submit(ctx, ports.SubmitResponseInput{
		SurveyID:      domain.SurveyID(req.GetSurveyId()),
		UserID:        domain.UserID(req.GetUserId()),
		ExamAttemptID: domain.AttemptID(req.GetExamAttemptId()),
		Answers:       answers,
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ResponseObjectResponse{Response: toProtoResponse(r)}, nil
}

func (h *ResponseHandler) GetResponse(ctx context.Context, req *pb.GetResponseRequest) (*pb.ResponseObjectResponse, error) {
	r, err := h.qrys.Get(ctx, domain.ResponseID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.ResponseObjectResponse{Response: toProtoResponse(r)}, nil
}

func (h *ResponseHandler) GetMetrics(ctx context.Context, req *pb.GetMetricsRequest) (*pb.MetricsResponse, error) {
	m, err := h.qrys.GetMetrics(ctx, domain.SurveyID(req.GetSurveyId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := &pb.MetricsResponse{
		SurveyId:       string(m.SurveyID),
		TotalResponses: m.TotalResponses,
		PerQuestion:    make([]*pb.QuestionMetrics, 0, len(m.PerQuestion)),
	}
	for _, qm := range m.PerQuestion {
		x := &pb.QuestionMetrics{
			QuestionId:   string(qm.QuestionID),
			Kind:         string(qm.Kind),
			Count:        qm.Count,
			Distribution: qm.Distribution,
		}
		if qm.Average != nil {
			x.Average = *qm.Average
			x.HasAverage = true
		}
		if qm.NPS != nil {
			x.Nps = *qm.NPS
			x.HasNps = true
		}
		out.PerQuestion = append(out.PerQuestion, x)
	}
	return out, nil
}

func toProtoResponse(r *domain.Response) *pb.Response {
	if r == nil {
		return nil
	}
	return &pb.Response{
		Id:            string(r.ID),
		SurveyId:      string(r.SurveyID),
		UserId:        string(r.UserID),
		ExamAttemptId: string(r.ExamAttemptID),
		SubmittedAt:   timestamppb.New(r.SubmittedAt),
	}
}

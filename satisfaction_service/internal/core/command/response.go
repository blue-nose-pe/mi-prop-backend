package command

import (
	"context"
	"time"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
)

type ResponseHandler struct {
	surveys   ports.SurveyRepository
	responses ports.ResponseRepository
}

var _ ports.ResponseCommands = (*ResponseHandler)(nil)

func NewResponseHandler(s ports.SurveyRepository, r ports.ResponseRepository) *ResponseHandler {
	return &ResponseHandler{surveys: s, responses: r}
}

func (h *ResponseHandler) Submit(ctx context.Context, in ports.SubmitResponseInput) (*domain.Response, error) {
	s, err := h.surveys.FindByID(ctx, in.SurveyID)
	if err != nil {
		return nil, err
	}
	if !s.Published || !s.Active {
		return nil, domain.ErrSurveyNotPublished
	}
	r := &domain.Response{
		SurveyID:      in.SurveyID,
		UserID:        in.UserID,
		ExamAttemptID: in.ExamAttemptID,
		SubmittedAt:   time.Now().UTC(),
	}
	answers := make([]domain.Answer, 0, len(in.Answers))
	for _, a := range in.Answers {
		answers = append(answers, domain.Answer{
			QuestionID:  a.QuestionID,
			ValueText:   a.ValueText,
			ValueNumber: a.ValueNumber,
		})
	}
	if err := h.responses.Submit(ctx, r, answers); err != nil {
		return nil, err
	}
	return r, nil
}

package command

import (
	"context"
	"time"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
)

type ResponseHandler struct {
	surveys   ports.SurveyRepository
	questions ports.QuestionRepository
	responses ports.ResponseRepository
}

var _ ports.ResponseCommands = (*ResponseHandler)(nil)

func NewResponseHandler(s ports.SurveyRepository, q ports.QuestionRepository, r ports.ResponseRepository) *ResponseHandler {
	return &ResponseHandler{surveys: s, questions: q, responses: r}
}

// Submit valida required + single-submission segun trigger_kind del survey:
//   - post_test:   unico por (user, survey, exam_attempt). Si no se manda
//                  exam_attempt_id no se aplica restriccion.
//   - on_demand:   unico por (user, survey). 1 sola respuesta por survey.
//   - recurring:   sin restriccion (ej: NPS mensual).
func (h *ResponseHandler) Submit(ctx context.Context, in ports.SubmitResponseInput) (*domain.Response, error) {
	s, err := h.surveys.FindByID(ctx, in.SurveyID)
	if err != nil {
		return nil, err
	}
	if !s.Published || !s.Active {
		return nil, domain.ErrSurveyNotPublished
	}

	qs, err := h.questions.ListBySurvey(ctx, in.SurveyID)
	if err != nil {
		return nil, err
	}
	provided := make(map[domain.QuestionID]bool, len(in.Answers))
	for _, a := range in.Answers {
		provided[a.QuestionID] = true
	}
	for _, q := range qs {
		if q.Required && !provided[q.ID] {
			return nil, domain.ErrMissingRequiredAnswer
		}
	}

	switch s.Trigger {
	case "post_test":
		if in.ExamAttemptID != "" {
			exists, err := h.responses.ExistsByUserAttempt(ctx, in.UserID, in.SurveyID, in.ExamAttemptID)
			if err != nil {
				return nil, err
			}
			if exists {
				return nil, domain.ErrAlreadySubmittedThisAttempt
			}
		}
	case "on_demand":
		exists, err := h.responses.ExistsByUserSurvey(ctx, in.UserID, in.SurveyID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, domain.ErrAlreadySubmittedThisSurvey
		}
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

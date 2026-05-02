// Package query — read side de satisfaction_service.
package query

import (
	"context"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
	"satisfaction_service/internal/shared/search"
)

type SurveyHandler struct {
	surveys   ports.SurveyRepository
	questions ports.QuestionRepository
}

var _ ports.SurveyQueries = (*SurveyHandler)(nil)

func NewSurveyHandler(s ports.SurveyRepository, q ports.QuestionRepository) *SurveyHandler {
	return &SurveyHandler{surveys: s, questions: q}
}

func (h *SurveyHandler) Get(ctx context.Context, id domain.SurveyID) (*domain.Survey, error) {
	return h.surveys.FindByID(ctx, id)
}

func (h *SurveyHandler) GetByCode(ctx context.Context, code string, version int32) (*domain.Survey, error) {
	return h.surveys.FindByCodeVersion(ctx, code, version)
}

func (h *SurveyHandler) ListQuestions(ctx context.Context, surveyID domain.SurveyID) ([]domain.Question, error) {
	return h.questions.ListBySurvey(ctx, surveyID)
}

func (h *SurveyHandler) Search(ctx context.Context, req search.Request) (*search.Response, error) {
	return h.surveys.Search(ctx, req)
}

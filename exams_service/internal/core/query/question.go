package query

import (
	"context"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
	"exams_service/internal/shared/search"
)

type QuestionHandler struct {
	questions ports.QuestionRepository
	options   ports.QuestionOptionRepository
}

var _ ports.QuestionQueries = (*QuestionHandler)(nil)

func NewQuestionHandler(
	questions ports.QuestionRepository,
	options ports.QuestionOptionRepository,
) *QuestionHandler {
	return &QuestionHandler{questions: questions, options: options}
}

func (h *QuestionHandler) Get(ctx context.Context, id domain.QuestionID) (*domain.Question, error) {
	return h.questions.FindByID(ctx, id)
}

func (h *QuestionHandler) ListOptions(ctx context.Context, qID domain.QuestionID) ([]domain.QuestionOption, error) {
	return h.options.ListByQuestion(ctx, qID)
}

func (h *QuestionHandler) Search(ctx context.Context, req search.Request) (*search.Response, error) {
	return h.questions.Search(ctx, req)
}

package query

import (
	"context"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

type ExamQuestionHandler struct {
	examQuestions ports.ExamQuestionRepository
}

var _ ports.ExamQuestionQueries = (*ExamQuestionHandler)(nil)

func NewExamQuestionHandler(eqRepo ports.ExamQuestionRepository) *ExamQuestionHandler {
	return &ExamQuestionHandler{examQuestions: eqRepo}
}

func (h *ExamQuestionHandler) List(ctx context.Context, examID domain.ExamID) ([]domain.ExamQuestion, error) {
	return h.examQuestions.List(ctx, examID)
}

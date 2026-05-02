// Package query — read side de exams_service. Sin efectos secundarios.
package query

import (
	"context"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
	"exams_service/internal/shared/search"
)

// ExamHandler implementa ports.ExamQueries.
type ExamHandler struct {
	exams ports.ExamRepository
}

var _ ports.ExamQueries = (*ExamHandler)(nil)

func NewExamHandler(exams ports.ExamRepository) *ExamHandler { return &ExamHandler{exams: exams} }

func (h *ExamHandler) Get(ctx context.Context, id domain.ExamID) (*domain.Exam, error) {
	return h.exams.FindByID(ctx, id)
}

func (h *ExamHandler) Search(ctx context.Context, req search.Request) (*search.Response, error) {
	return h.exams.Search(ctx, req)
}

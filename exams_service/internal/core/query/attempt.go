package query

import (
	"context"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

type AttemptHandler struct {
	attempts ports.AttemptRepository
}

var _ ports.AttemptQueries = (*AttemptHandler)(nil)

func NewAttemptHandler(attempts ports.AttemptRepository) *AttemptHandler {
	return &AttemptHandler{attempts: attempts}
}

func (h *AttemptHandler) Get(ctx context.Context, id domain.AttemptID) (*domain.ExamAttempt, error) {
	return h.attempts.FindByID(ctx, id)
}

func (h *AttemptHandler) ListByUser(ctx context.Context, userID domain.UserID) ([]domain.ExamAttempt, error) {
	return h.attempts.ListByUser(ctx, userID)
}

func (h *AttemptHandler) ListByExam(ctx context.Context, examID domain.ExamID) ([]domain.ExamAttempt, error) {
	return h.attempts.ListByExam(ctx, examID)
}

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

func (h *AttemptHandler) GetActiveByExamUser(ctx context.Context, examID domain.ExamID, userID domain.UserID) (*domain.ExamAttempt, error) {
	return h.attempts.FindActiveByExamUser(ctx, examID, userID)
}

func (h *AttemptHandler) ListByUser(ctx context.Context, userID domain.UserID) ([]domain.ExamAttempt, error) {
	return h.attempts.ListByUser(ctx, userID)
}

func (h *AttemptHandler) ListByExam(ctx context.Context, examID domain.ExamID) ([]domain.ExamAttempt, error) {
	return h.attempts.ListByExam(ctx, examID)
}

func (h *AttemptHandler) ListByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.ExamAttempt, error) {
	return h.attempts.ListByColegio(ctx, schoolID)
}

func (h *AttemptHandler) ListByKey(ctx context.Context, keyID domain.KeyID) ([]domain.ExamAttempt, error) {
	return h.attempts.ListByKey(ctx, keyID)
}

func (h *AttemptHandler) ListEnrichedAnswers(ctx context.Context, id domain.AttemptID) ([]domain.EnrichedAnswer, error) {
	return h.attempts.ListEnrichedAnswers(ctx, id)
}

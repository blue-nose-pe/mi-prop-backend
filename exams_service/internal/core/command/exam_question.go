package command

import (
	"context"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

// ExamQuestionHandler maneja la asociación N:M.
// Bloquea modificaciones cuando el exam ya fue publicado (consistencia
// con la regla "publicado = inmutable; clonar para editar").
type ExamQuestionHandler struct {
	exams         ports.ExamRepository
	examQuestions ports.ExamQuestionRepository
}

var _ ports.ExamQuestionCommands = (*ExamQuestionHandler)(nil)

func NewExamQuestionHandler(
	exams ports.ExamRepository,
	examQuestions ports.ExamQuestionRepository,
) *ExamQuestionHandler {
	return &ExamQuestionHandler{exams: exams, examQuestions: examQuestions}
}

func (h *ExamQuestionHandler) Add(ctx context.Context, in ports.AddExamQuestionInput) error {
	if err := h.assertEditable(ctx, in.ExamID); err != nil {
		return err
	}
	return h.examQuestions.Add(ctx, &domain.ExamQuestion{
		ExamID:     in.ExamID,
		QuestionID: in.QuestionID,
		Points:     in.Points,
		SortOrder:  in.SortOrder,
	})
}

func (h *ExamQuestionHandler) Remove(ctx context.Context, examID domain.ExamID, questionID domain.QuestionID) error {
	if err := h.assertEditable(ctx, examID); err != nil {
		return err
	}
	return h.examQuestions.Remove(ctx, examID, questionID)
}

func (h *ExamQuestionHandler) Reorder(ctx context.Context, examID domain.ExamID, ordered []domain.QuestionID) error {
	if err := h.assertEditable(ctx, examID); err != nil {
		return err
	}
	return h.examQuestions.Reorder(ctx, examID, ordered)
}

func (h *ExamQuestionHandler) assertEditable(ctx context.Context, examID domain.ExamID) error {
	e, err := h.exams.FindByID(ctx, examID)
	if err != nil {
		return err
	}
	if e.Published {
		return domain.ErrCannotEditPublished
	}
	return nil
}

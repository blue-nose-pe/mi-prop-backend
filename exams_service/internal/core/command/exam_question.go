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

// Reorder requiere que `ordered` sea una permutación exacta del set de
// question_ids del exam (preserva la unicidad de sort_order). Devuelve
// ErrInvalidReorder si no.
func (h *ExamQuestionHandler) Reorder(ctx context.Context, examID domain.ExamID, ordered []domain.QuestionID) error {
	if err := h.assertEditable(ctx, examID); err != nil {
		return err
	}
	current, err := h.examQuestions.List(ctx, examID)
	if err != nil {
		return err
	}
	if !sameQuestionSet(current, ordered) {
		return domain.ErrInvalidReorder
	}
	return h.examQuestions.Reorder(ctx, examID, ordered)
}

func sameQuestionSet(current []domain.ExamQuestion, ordered []domain.QuestionID) bool {
	if len(current) != len(ordered) {
		return false
	}
	want := make(map[domain.QuestionID]struct{}, len(current))
	for _, eq := range current {
		want[eq.QuestionID] = struct{}{}
	}
	seen := make(map[domain.QuestionID]struct{}, len(ordered))
	for _, qID := range ordered {
		if _, ok := want[qID]; !ok {
			return false
		}
		if _, dup := seen[qID]; dup {
			return false
		}
		seen[qID] = struct{}{}
	}
	return true
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

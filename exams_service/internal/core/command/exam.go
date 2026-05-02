package command

import (
	"context"
	"strings"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

// ExamHandler implementa ports.ExamCommands. Mutaciones sobre exam.
//
// Reglas de negocio:
//   - Update solo aplica a exámenes NO publicados (preserva inmutabilidad
//     post-publicación). Para "editar" un exam publicado, hay que Clone().
//   - Publish requiere que el exam tenga al menos una pregunta (lo verifica
//     a través de exam_question repo).
//   - Clone copia el exam y sus preguntas → genera versión nueva con
//     parent_exam_id apuntando al original. Histórico preservado.
type ExamHandler struct {
	types         ports.ExamTypeRepository
	exams         ports.ExamRepository
	examQuestions ports.ExamQuestionRepository
}

var _ ports.ExamCommands = (*ExamHandler)(nil)

func NewExamHandler(
	types ports.ExamTypeRepository,
	exams ports.ExamRepository,
	examQuestions ports.ExamQuestionRepository,
) *ExamHandler {
	return &ExamHandler{types: types, exams: exams, examQuestions: examQuestions}
}

func (h *ExamHandler) Create(ctx context.Context, in ports.CreateExamInput) (*domain.Exam, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, domain.ErrEmptyExamName
	}
	if !in.StartAt.Before(in.EndAt) {
		return nil, domain.ErrInvalidDateRange
	}

	t, err := h.types.FindByCode(ctx, strings.ToLower(strings.TrimSpace(in.ExamTypeCode)))
	if err != nil {
		return nil, err
	}

	e := &domain.Exam{
		ExamTypeID:      t.ID,
		SchoolID:        in.SchoolID,
		Name:            strings.TrimSpace(in.Name),
		StartAt:         in.StartAt,
		EndAt:           in.EndAt,
		MaxParticipants: in.MaxParticipants,
		Version:         1,
		Active:          true,
		Published:       false,
	}
	id, err := h.exams.Save(ctx, e)
	if err != nil {
		return nil, err
	}
	e.ID = id
	return e, nil
}

func (h *ExamHandler) Update(ctx context.Context, in ports.UpdateExamInput) (*domain.Exam, error) {
	e, err := h.exams.FindByID(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if e.Published {
		return nil, domain.ErrCannotEditPublished
	}
	if v := strings.TrimSpace(in.Name); v != "" {
		e.Name = v
	}
	if !in.StartAt.IsZero() {
		e.StartAt = in.StartAt
	}
	if !in.EndAt.IsZero() {
		e.EndAt = in.EndAt
	}
	if in.MaxParticipants >= 0 {
		e.MaxParticipants = in.MaxParticipants
	}
	if !e.StartAt.Before(e.EndAt) {
		return nil, domain.ErrInvalidDateRange
	}
	if err := h.exams.Update(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

func (h *ExamHandler) Publish(ctx context.Context, id domain.ExamID) error {
	if _, err := h.exams.FindByID(ctx, id); err != nil {
		return err
	}
	qs, err := h.examQuestions.List(ctx, id)
	if err != nil {
		return err
	}
	if len(qs) == 0 {
		return domain.ErrEmptyExamName // reuse — un publicar sin preguntas es inválido
	}
	return h.exams.SetPublished(ctx, id, true)
}

func (h *ExamHandler) Deactivate(ctx context.Context, id domain.ExamID) error {
	return h.exams.SetActive(ctx, id, false)
}

// Clone genera una nueva versión del exam (parent_exam_id = id) con
// version+1, copiando todas sus exam_question. Permite "editar" un exam
// publicado sin perder los attempts antiguos.
func (h *ExamHandler) Clone(ctx context.Context, id domain.ExamID) (*domain.Exam, error) {
	src, err := h.exams.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	clone := &domain.Exam{
		ExamTypeID:      src.ExamTypeID,
		SchoolID:        src.SchoolID,
		ParentExamID:    src.ID,
		Version:         src.Version + 1,
		Name:            src.Name,
		StartAt:         src.StartAt,
		EndAt:           src.EndAt,
		MaxParticipants: src.MaxParticipants,
		Active:          true,
		Published:       false, // la nueva versión nace en draft
	}
	newID, err := h.exams.Save(ctx, clone)
	if err != nil {
		return nil, err
	}
	clone.ID = newID
	if err := h.examQuestions.CloneInto(ctx, src.ID, newID); err != nil {
		return nil, err
	}
	return clone, nil
}

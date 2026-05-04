package command

import (
	"context"
	"strings"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

// QuestionHandler implementa ports.QuestionCommands.
type QuestionHandler struct {
	questions ports.QuestionRepository
	options   ports.QuestionOptionRepository
}

var _ ports.QuestionCommands = (*QuestionHandler)(nil)

func NewQuestionHandler(
	questions ports.QuestionRepository,
	options ports.QuestionOptionRepository,
) *QuestionHandler {
	return &QuestionHandler{questions: questions, options: options}
}

func (h *QuestionHandler) Create(ctx context.Context, in ports.CreateQuestionInput) (*domain.Question, error) {
	q := &domain.Question{
		Text:     strings.TrimSpace(in.Text),
		Category: strings.TrimSpace(in.Category),
		Active:   true,
	}
	id, err := h.questions.Save(ctx, q)
	if err != nil {
		return nil, err
	}
	return h.questions.FindByID(ctx, id)
}

func (h *QuestionHandler) Update(ctx context.Context, in ports.UpdateQuestionInput) (*domain.Question, error) {
	q, err := h.questions.FindByID(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if v := strings.TrimSpace(in.Text); v != "" {
		q.Text = v
	}
	if v := strings.TrimSpace(in.Category); v != "" {
		q.Category = v
	}
	if err := h.questions.Update(ctx, q); err != nil {
		return nil, err
	}
	return q, nil
}

func (h *QuestionHandler) Deactivate(ctx context.Context, id domain.QuestionID) error {
	return h.questions.SetActive(ctx, id, false)
}

func (h *QuestionHandler) AddOption(ctx context.Context, in ports.AddOptionInput) (*domain.QuestionOption, error) {
	if _, err := h.questions.FindByID(ctx, in.QuestionID); err != nil {
		return nil, err
	}
	o := &domain.QuestionOption{
		QuestionID: in.QuestionID,
		Text:       strings.TrimSpace(in.Text),
		IsCorrect:  in.IsCorrect,
		SortOrder:  in.SortOrder,
	}
	id, err := h.options.Save(ctx, o)
	if err != nil {
		return nil, err
	}
	o.ID = id
	return o, nil
}

func (h *QuestionHandler) UpdateOption(ctx context.Context, in ports.UpdateOptionInput) error {
	o := &domain.QuestionOption{
		ID:        in.ID,
		Text:      strings.TrimSpace(in.Text),
		IsCorrect: in.IsCorrect,
		SortOrder: in.SortOrder,
	}
	return h.options.Update(ctx, o)
}

func (h *QuestionHandler) RemoveOption(ctx context.Context, id domain.OptionID) error {
	return h.options.Delete(ctx, id)
}

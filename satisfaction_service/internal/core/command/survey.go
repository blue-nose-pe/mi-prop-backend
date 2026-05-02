// Package command — write side de satisfaction_service.
package command

import (
	"context"
	"strings"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
)

type SurveyHandler struct {
	surveys   ports.SurveyRepository
	questions ports.QuestionRepository
}

var _ ports.SurveyCommands = (*SurveyHandler)(nil)

func NewSurveyHandler(s ports.SurveyRepository, q ports.QuestionRepository) *SurveyHandler {
	return &SurveyHandler{surveys: s, questions: q}
}

func (h *SurveyHandler) Create(ctx context.Context, in ports.CreateSurveyInput) (*domain.Survey, error) {
	if !validRole(in.TargetRole) {
		return nil, domain.ErrInvalidTarget
	}
	if !validTrigger(in.Trigger) {
		return nil, domain.ErrInvalidTrigger
	}
	s := &domain.Survey{
		Code:        strings.TrimSpace(in.Code),
		Title:       strings.TrimSpace(in.Title),
		Description: strings.TrimSpace(in.Description),
		TargetRole:  in.TargetRole,
		Trigger:     in.Trigger,
		Version:     1,
		Active:      true,
		Published:   false,
	}
	id, err := h.surveys.Save(ctx, s)
	if err != nil {
		return nil, err
	}
	s.ID = id
	return s, nil
}

func (h *SurveyHandler) Update(ctx context.Context, in ports.UpdateSurveyInput) (*domain.Survey, error) {
	s, err := h.surveys.FindByID(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	if s.Published {
		return nil, domain.ErrCannotEditPublished
	}
	if v := strings.TrimSpace(in.Title); v != "" {
		s.Title = v
	}
	if in.Description != "" {
		s.Description = in.Description
	}
	if err := h.surveys.Update(ctx, s); err != nil {
		return nil, err
	}
	return s, nil
}

func (h *SurveyHandler) Publish(ctx context.Context, id domain.SurveyID) error {
	if _, err := h.surveys.FindByID(ctx, id); err != nil {
		return err
	}
	qs, err := h.questions.ListBySurvey(ctx, id)
	if err != nil {
		return err
	}
	if len(qs) == 0 {
		return domain.ErrSurveyNotFound // un survey sin preguntas no se publica
	}
	return h.surveys.SetPublished(ctx, id, true)
}

func (h *SurveyHandler) Deactivate(ctx context.Context, id domain.SurveyID) error {
	return h.surveys.SetActive(ctx, id, false)
}

func (h *SurveyHandler) AddQuestion(ctx context.Context, in ports.AddQuestionInput) (*domain.Question, error) {
	if !in.Kind.Valid() {
		return nil, domain.ErrInvalidQuestionKind
	}
	s, err := h.surveys.FindByID(ctx, in.SurveyID)
	if err != nil {
		return nil, err
	}
	if s.Published {
		return nil, domain.ErrCannotEditPublished
	}
	q := &domain.Question{
		SurveyID:    in.SurveyID,
		Text:        strings.TrimSpace(in.Text),
		Kind:        in.Kind,
		SortOrder:   in.SortOrder,
		OptionsJSON: in.OptionsJSON,
		Required:    in.Required,
	}
	id, err := h.questions.Save(ctx, q)
	if err != nil {
		return nil, err
	}
	q.ID = id
	return q, nil
}

func (h *SurveyHandler) UpdateQuestion(ctx context.Context, in ports.UpdateQuestionInput) error {
	q, err := h.questions.FindByID(ctx, in.ID)
	if err != nil {
		return err
	}
	s, err := h.surveys.FindByID(ctx, q.SurveyID)
	if err != nil {
		return err
	}
	if s.Published {
		return domain.ErrCannotEditPublished
	}
	if v := strings.TrimSpace(in.Text); v != "" {
		q.Text = v
	}
	q.SortOrder = in.SortOrder
	q.OptionsJSON = in.OptionsJSON
	q.Required = in.Required
	return h.questions.Update(ctx, q)
}

func (h *SurveyHandler) RemoveQuestion(ctx context.Context, id domain.QuestionID) error {
	q, err := h.questions.FindByID(ctx, id)
	if err != nil {
		return err
	}
	s, err := h.surveys.FindByID(ctx, q.SurveyID)
	if err != nil {
		return err
	}
	if s.Published {
		return domain.ErrCannotEditPublished
	}
	return h.questions.Delete(ctx, id)
}

func validRole(r string) bool {
	switch r {
	case "admin", "asesor", "coordinador", "estudiante":
		return true
	}
	return false
}

func validTrigger(t string) bool {
	switch t {
	case "post_test", "on_demand", "recurring":
		return true
	}
	return false
}

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
		KeyID:       strings.TrimSpace(in.KeyID),
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
	// El bloqueo "published no editable" protege el CONTENIDO (titulo,
	// descripcion, trigger, preguntas) para no mutar la version que los
	// alumnos estan respondiendo. PERO key_id es TARGETING (a que key se
	// dirige la encuesta), no contenido: debe poder cambiarse aun publicada.
	// El flujo "elegir encuesta al crear la key" ata una encuesta PUBLICADA
	// a la nueva key; sin esta excepcion el PATCH daba 403 SURVEY_LOCKED y la
	// encuesta nunca quedaba atada (el front lo hace fire-and-forget).
	editsContent := strings.TrimSpace(in.Title) != "" ||
		in.Description != "" ||
		strings.TrimSpace(in.Trigger) != ""
	if s.Published && editsContent {
		return nil, domain.ErrCannotEditPublished
	}
	if v := strings.TrimSpace(in.Title); v != "" {
		s.Title = v
	}
	if in.Description != "" {
		s.Description = in.Description
	}
	// Cliente: trigger_kind editable post-creacion. "" = no tocar (mantiene
	// el valor previo). Permite cambiar a que tipo de examen aplica la
	// encuesta despues de crearla — el survey-builder usa esto para
	// guardar el dropdown "Aplicar a".
	if v := strings.TrimSpace(in.Trigger); v != "" {
		s.Trigger = v
	}
	if err := h.surveys.Update(ctx, s); err != nil {
		return nil, err
	}
	// key_id ata ESTA encuesta a una key via la tabla survey_key (modelo
	// key -> survey, migration 007): muchas keys pueden compartir la misma
	// encuesta. Un uuid ata; "" = no tocar; "-" (limpiar legacy del modelo
	// survey.key_id viejo) ya no aplica -> no-op. NO tocamos s.KeyID (columna
	// legacy deprecada).
	if v := strings.TrimSpace(in.KeyID); v != "" && v != "-" {
		if err := h.surveys.AssignKeyToSurvey(ctx, s.ID, v); err != nil {
			return nil, err
		}
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
	if err := h.surveys.SetPublished(ctx, id, true); err != nil {
		return err
	}
	// Publicar tambien REACTIVA (active=true): un survey pausado que se
	// publica debe quedar vivo. Sin esto el boton "play" en un survey
	// pausado no hacia nada (seguia paused). Mismo fix que exams.
	return h.surveys.SetActive(ctx, id, true)
}

func (h *SurveyHandler) Deactivate(ctx context.Context, id domain.SurveyID) error {
	return h.surveys.SetActive(ctx, id, false)
}

// Reactivate vuelve un survey paused (active=false) a draft activo. NO
// republica — el usuario debe pasar por Publish explicitamente despues.
func (h *SurveyHandler) Reactivate(ctx context.Context, id domain.SurveyID) (*domain.Survey, error) {
	s, err := h.surveys.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := h.surveys.SetActive(ctx, id, true); err != nil {
		return nil, err
	}
	// Devolvemos un Survey "actualizado" sin re-leer; el caller ya tiene
	// el id y suele recargar la lista despues.
	s.Active = true
	s.Published = false
	return s, nil
}

// Clone crea un draft nuevo con titulo "<orig> (copia)", code suffixeado
// y mismo set de preguntas. Util cuando el survey original ya esta
// published y no permite mas ediciones. La copia siempre arranca
// active=true, published=false.
func (h *SurveyHandler) Clone(ctx context.Context, id domain.SurveyID) (*domain.Survey, error) {
	orig, err := h.surveys.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	qs, err := h.questions.ListBySurvey(ctx, id)
	if err != nil {
		return nil, err
	}
	suffix := strings.ToLower(string(id))
	if len(suffix) > 6 {
		suffix = suffix[:6]
	}
	cloneCode := orig.Code + "-copy-" + suffix
	clone := &domain.Survey{
		Code:        cloneCode,
		Title:       orig.Title + " (copia)",
		Description: orig.Description,
		TargetRole:  orig.TargetRole,
		Trigger:     orig.Trigger,
		KeyID:       orig.KeyID,
		Version:     orig.Version + 1,
		Active:      true,
		Published:   false,
	}
	newID, err := h.surveys.Save(ctx, clone)
	if err != nil {
		return nil, err
	}
	clone.ID = newID
	for _, q := range qs {
		nq := &domain.Question{
			SurveyID:    newID,
			Text:        q.Text,
			Kind:        q.Kind,
			SortOrder:   q.SortOrder,
			OptionsJSON: q.OptionsJSON,
			Required:    q.Required,
		}
		if _, err := h.questions.Save(ctx, nq); err != nil {
			return nil, err
		}
	}
	return clone, nil
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

// validTrigger acepta los valores de timing legacy (post_test/on_demand/
// recurring) y, desde el rediseño del survey-builder, los tipos de examen
// a los que la encuesta "Aplica a" (simulacro/vocacional/estilos). Ver
// migration 005_survey_applies_to.sql.
func validTrigger(t string) bool {
	switch t {
	case "post_test", "on_demand", "recurring",
		"simulacro", "vocacional", "estilos":
		return true
	}
	return false
}

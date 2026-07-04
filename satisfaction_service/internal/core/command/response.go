package command

import (
	"context"
	"strings"
	"time"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
)

type ResponseHandler struct {
	surveys   ports.SurveyRepository
	questions ports.QuestionRepository
	responses ports.ResponseRepository
}

var _ ports.ResponseCommands = (*ResponseHandler)(nil)

func NewResponseHandler(s ports.SurveyRepository, q ports.QuestionRepository, r ports.ResponseRepository) *ResponseHandler {
	return &ResponseHandler{surveys: s, questions: q, responses: r}
}

// Submit valida required + single-submission segun trigger_kind del survey:
//   - post_test:   unico por (user, survey, exam_attempt). Si no se manda
//                  exam_attempt_id no se aplica restriccion.
//   - on_demand:   unico por (user, survey). 1 sola respuesta por survey.
//   - recurring:   sin restriccion (ej: NPS mensual).
func (h *ResponseHandler) Submit(ctx context.Context, in ports.SubmitResponseInput) (*domain.Response, error) {
	s, err := h.surveys.FindByID(ctx, in.SurveyID)
	if err != nil {
		return nil, err
	}
	if !s.Published || !s.Active {
		return nil, domain.ErrSurveyNotPublished
	}

	qs, err := h.questions.ListBySurvey(ctx, in.SurveyID)
	if err != nil {
		return nil, err
	}
	// Validación de contenido (cubre TODOS los caminos: público anónimo y
	// autenticado). Antes el endpoint público aceptaba cualquier question_id y
	// cualquier value_number (un NPS de 15 contaba como promotor; un scale de
	// 100 daba CSAT >100%). Ahora:
	//   - rechazamos answers cuyo question_id NO pertenece a la encuesta,
	//   - scale exige 1..5, nps exige 0..10,
	//   - una pregunta required de texto (open/single/multi) exige contenido.
	qByID := make(map[domain.QuestionID]domain.Question, len(qs))
	for _, q := range qs {
		qByID[q.ID] = q
	}
	answeredOK := make(map[domain.QuestionID]bool, len(in.Answers))
	for _, a := range in.Answers {
		q, ok := qByID[a.QuestionID]
		if !ok {
			return nil, domain.ErrQuestionNotInSurvey
		}
		switch q.Kind {
		case domain.KindScale:
			if a.ValueNumber == nil || *a.ValueNumber < 1 || *a.ValueNumber > 5 {
				return nil, domain.ErrInvalidAnswerValue
			}
		case domain.KindNPS:
			if a.ValueNumber == nil || *a.ValueNumber < 0 || *a.ValueNumber > 10 {
				return nil, domain.ErrInvalidAnswerValue
			}
		default: // open / single / multi → contenido de texto
			if strings.TrimSpace(a.ValueText) == "" {
				return nil, domain.ErrInvalidAnswerValue
			}
		}
		answeredOK[a.QuestionID] = true
	}
	for _, q := range qs {
		if q.Required && !answeredOK[q.ID] {
			return nil, domain.ErrMissingRequiredAnswer
		}
	}

	// Dedup de respuesta única. Antes el switch comparaba contra 'post_test'/
	// 'on_demand' (timing legacy) pero trigger_kind hoy guarda el TIPO de examen
	// (simulacro/vocacional/estilos) → el guard nunca disparaba y el único freno
	// era el localStorage del navegador (doble-click, otra pestaña, incógnito).
	// Ahora: única por (user, attempt) si hay intento, si no por (user, survey);
	// y para el flujo anónimo (sin user) única por (survey, attempt). 'recurring'
	// queda SIN restricción (NPS mensual: se responde varias veces a propósito).
	if s.Trigger != "recurring" {
		if in.UserID != "" {
			if in.ExamAttemptID != "" {
				exists, err := h.responses.ExistsByUserAttempt(ctx, in.UserID, in.SurveyID, in.ExamAttemptID)
				if err != nil {
					return nil, err
				}
				if exists {
					return nil, domain.ErrAlreadySubmittedThisAttempt
				}
			} else {
				exists, err := h.responses.ExistsByUserSurvey(ctx, in.UserID, in.SurveyID)
				if err != nil {
					return nil, err
				}
				if exists {
					return nil, domain.ErrAlreadySubmittedThisSurvey
				}
			}
		} else if in.ExamAttemptID != "" {
			// Anónimo: no hay user, pero el intento identifica la sesión. Evita
			// que un mismo intento infle las cifras con reenvíos.
			exists, err := h.responses.ExistsBySurveyAttempt(ctx, in.SurveyID, in.ExamAttemptID)
			if err != nil {
				return nil, err
			}
			if exists {
				return nil, domain.ErrAlreadySubmittedThisAttempt
			}
		}
	}

	r := &domain.Response{
		SurveyID:      in.SurveyID,
		UserID:        in.UserID,
		ExamAttemptID: in.ExamAttemptID,
		SubmittedAt:   time.Now().UTC(),
	}
	answers := make([]domain.Answer, 0, len(in.Answers))
	for _, a := range in.Answers {
		answers = append(answers, domain.Answer{
			QuestionID:  a.QuestionID,
			ValueText:   a.ValueText,
			ValueNumber: a.ValueNumber,
		})
	}
	if err := h.responses.Submit(ctx, r, answers); err != nil {
		return nil, err
	}
	return r, nil
}

// Package command — write side de exams_service.
package command

import (
	"context"
	"time"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

// AttemptHandler implementa ports.AttemptCommands.
//
// Reglas de negocio:
//   - Start verifica que el exam esté Open (publicado, activo, ventana
//     temporal vigente) y que aforo no esté lleno.
//   - Si KeyID llega no-vacío, se delega a keys_service para validar el
//     código y registrar el uso (esto se hace en el handler gRPC, no
//     acá; el core ya recibe el KeyID validado).
//   - Answer: idempotente — un attempt elige UNA opción por pregunta.
//     Si ya había respuesta para esa pregunta, se sobreescribe.
//   - Finish: calcula score sumando puntos de respuestas correctas.
//     Una vez submitted_at != NULL, no se puede ni Answer ni Finish.
type AttemptHandler struct {
	exams         ports.ExamRepository
	examQuestions ports.ExamQuestionRepository
	options       ports.QuestionOptionRepository
	attempts      ports.AttemptRepository
}

var _ ports.AttemptCommands = (*AttemptHandler)(nil)

func NewAttemptHandler(
	exams ports.ExamRepository,
	examQuestions ports.ExamQuestionRepository,
	options ports.QuestionOptionRepository,
	attempts ports.AttemptRepository,
) *AttemptHandler {
	return &AttemptHandler{
		exams:         exams,
		examQuestions: examQuestions,
		options:       options,
		attempts:      attempts,
	}
}

func (h *AttemptHandler) Start(ctx context.Context, in ports.StartAttemptInput) (*ports.StartAttemptResult, error) {
	// Idempotencia: si el user ya tiene un attempt sin submit para este
	// exam, devolverlo en lugar de crear uno nuevo. Sin esto, un refresh
	// del front creaba un attempt extra + un uso extra de la key (Bug #2).
	if existing, err := h.attempts.FindActiveByExamUser(ctx, in.ExamID, in.UserID); err != nil {
		return nil, err
	} else if existing != nil {
		return &ports.StartAttemptResult{Attempt: existing, Reused: true}, nil
	}

	e, err := h.exams.FindByID(ctx, in.ExamID)
	if err != nil {
		return nil, err
	}
	if !e.IsOpen(time.Now()) {
		if !e.Published {
			return nil, domain.ErrExamNotPublished
		}
		return nil, domain.ErrExamClosed
	}

	// Aforo a nivel exam (e.MaxParticipants): solo aplica cuando NO hay
	// key. Si hay key, el aforo lo dicta la key (keys_service.IncrementUsage
	// es atomico y se valida antes de llamar a Start desde el handler gRPC),
	// asi evitamos doble fuente de verdad inconsistente (Bug #3).
	if in.KeyID == "" && e.MaxParticipants > 0 {
		count, err := h.attempts.CountActiveByExam(ctx, e.ID)
		if err != nil {
			return nil, err
		}
		if count >= e.MaxParticipants {
			return nil, domain.ErrExamClosed
		}
	}

	att := &domain.ExamAttempt{
		ExamID:    e.ID,
		UserID:    in.UserID,
		KeyID:     in.KeyID,
		StartedAt: time.Now().UTC(),
	}
	id, err := h.attempts.Save(ctx, att)
	if err != nil {
		return nil, err
	}
	att.ID = id
	return &ports.StartAttemptResult{Attempt: att, Reused: false}, nil
}

func (h *AttemptHandler) Answer(ctx context.Context, in ports.AnswerInput) error {
	att, err := h.attempts.FindByID(ctx, in.AttemptID)
	if err != nil {
		return err
	}
	if att.SubmittedAt != nil {
		return domain.ErrAttemptAlreadyDone
	}
	opts, err := h.options.ListByQuestion(ctx, in.QuestionID)
	if err != nil {
		return err
	}
	valid := false
	for _, o := range opts {
		if o.ID == in.OptionID {
			valid = true
			break
		}
	}
	if !valid {
		return domain.ErrAnswerOptionMismatch
	}
	return h.attempts.UpsertAnswer(ctx, &domain.AttemptAnswer{
		AttemptID:  att.ID,
		QuestionID: in.QuestionID,
		OptionID:   in.OptionID,
		AnsweredAt: time.Now().UTC(),
	})
}

func (h *AttemptHandler) Finish(ctx context.Context, id domain.AttemptID) (*domain.ExamAttempt, error) {
	att, err := h.attempts.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if att.SubmittedAt != nil {
		return att, nil // idempotente: ya estaba finalizado
	}

	// Calcular score: cargar exam_questions (puntos por pregunta) y
	// respuestas; cruzar con question_option.is_correct.
	eqs, err := h.examQuestions.List(ctx, att.ExamID)
	if err != nil {
		return nil, err
	}
	answers, err := h.attempts.ListAnswers(ctx, id)
	if err != nil {
		return nil, err
	}

	pointsByQuestion := make(map[domain.QuestionID]int32, len(eqs))
	maxScore := int32(0)
	for _, eq := range eqs {
		pointsByQuestion[eq.QuestionID] = eq.Points
		maxScore += eq.Points
	}

	var score int32
	for _, a := range answers {
		opts, err := h.options.ListByQuestion(ctx, a.QuestionID)
		if err != nil {
			return nil, err
		}
		for _, o := range opts {
			if o.ID == a.OptionID && o.IsCorrect {
				score += pointsByQuestion[a.QuestionID]
				break
			}
		}
	}

	now := time.Now().UTC()
	if err := h.attempts.Finish(ctx, id, score, maxScore, now); err != nil {
		return nil, err
	}
	att.Score = &score
	att.MaxScore = &maxScore
	att.SubmittedAt = &now
	return att, nil
}

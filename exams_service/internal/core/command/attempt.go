// Package command — write side de exams_service.
package command

import (
	"context"
	"errors"
	"strings"
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
	// questions: necesario para resolver el kind de cada pregunta y
	// aplicar el scoring correcto en Finish (MULTIPLE_CHOICE = set-match
	// exacto, SCALE/OPEN_TEXT = sin score, etc.).
	questions     ports.QuestionRepository
	attempts      ports.AttemptRepository
}

var _ ports.AttemptCommands = (*AttemptHandler)(nil)

func NewAttemptHandler(
	exams ports.ExamRepository,
	examQuestions ports.ExamQuestionRepository,
	options ports.QuestionOptionRepository,
	questions ports.QuestionRepository,
	attempts ports.AttemptRepository,
) *AttemptHandler {
	return &AttemptHandler{
		exams:         exams,
		examQuestions: examQuestions,
		options:       options,
		questions:     questions,
		attempts:      attempts,
	}
}

// answerGrace tolera latencia de red y skew de reloj para no rechazar
// respuestas legítimas de último segundo. El enforcement real bloquea a quien
// sigue respondiendo MUCHO después del límite (cliente manipulado).
const answerGrace = 90 * time.Second

// durationMinutesFor devuelve el límite de tiempo (minutos) del intento según
// el exam. Replica las duraciones de prod/front: simulacro UCSP = 150,
// Nacional (code SIME-NAC-*) = 180; vocacional/estilos = 0 (sin límite, son
// tests de tendencia sin presión de tiempo).
func durationMinutesFor(e *domain.Exam) int32 {
	if e.ExamTypeID != 2 { // 2 = simulacro; 1=vocacional, 3=estilos no tienen timer
		return 0
	}
	if strings.HasPrefix(strings.ToUpper(e.Code), "SIME-NAC") {
		return 180
	}
	return 150 // UCSP (default simulacro)
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
	// Fix A: cross-type guard. Si el caller paso una key (Validate seteo
	// ExpectedExamTypeID con el exam_type_id de la key), exigir que matchee
	// el del exam. Esto impide que una key vocacional dispare un simulacro,
	// que en el UI quedaba como "Simulacro - Matematicas - Basico" para
	// preguntas RIASEC (scoring nonsense).
	if in.ExpectedExamTypeID != 0 && e.ExamTypeID != in.ExpectedExamTypeID {
		return nil, domain.ErrKeyExamTypeMismatch
	}

	// Candado masivo: una key LAN/masiva (con código pero sin colegio) SOLO
	// habilita el simulacro UCSP, nunca el Examen Nacional. El front ya solo
	// ofrece UCSP en el masivo; esto es defense-in-depth por si llega una
	// request armada a mano con un exam_id Nacional (code SIME-NAC-*).
	if in.KeyIsMasivo && strings.HasPrefix(strings.ToUpper(e.Code), "SIME-NAC") {
		return nil, domain.ErrMasivoUCSPOnly
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
		// Timer server-side: lockeamos la duración del intento al iniciar.
		DurationMinutes: durationMinutesFor(e),
	}
	id, err := h.attempts.Save(ctx, att)
	if err != nil {
		// Race: otro request concurrente del mismo alumno gano la insercion
		// del attempt activo (filtered UNIQUE uq_exam_attempt_active). Hacemos
		// retry-find y devolvemos ese attempt como Reused para que el cliente
		// vea exactamente el mismo resultado que si ese request hubiera llegado
		// primero — sin error, sin attempt duplicado.
		if errors.Is(err, ports.ErrConcurrentActiveAttempt) {
			winner, ferr := h.attempts.FindActiveByExamUser(ctx, in.ExamID, in.UserID)
			if ferr != nil {
				return nil, ferr
			}
			if winner != nil {
				return &ports.StartAttemptResult{Attempt: winner, Reused: true}, nil
			}
		}
		return nil, err
	}
	att.ID = id
	return &ports.StartAttemptResult{Attempt: att, Reused: false}, nil
}

// Answer persiste la respuesta del alumno a una pregunta. Soporta los 5
// kinds:
//   - SINGLE_CHOICE / TRUE_FALSE / SCALE: in.OptionID (single)
//   - MULTIPLE_CHOICE: in.OptionIDs (slice, todas marcadas)
//   - OPEN_TEXT: in.AnswerText (texto libre, sin option)
//
// Las opciones se validan contra la pregunta para evitar inyectar option
// ids de OTRA pregunta. Para OPEN_TEXT no hay opciones que validar.
func (h *AttemptHandler) Answer(ctx context.Context, in ports.AnswerInput) error {
	att, err := h.attempts.FindByID(ctx, in.AttemptID)
	if err != nil {
		return err
	}
	if att.SubmittedAt != nil {
		return domain.ErrAttemptAlreadyDone
	}

	// Timer server-side: si el intento tiene límite (simulacro), rechazar
	// respuestas que llegan pasado started_at + duration + gracia. La gracia
	// tolera latencia/skew para no rechazar respuestas legítimas de último
	// segundo; un cliente manipulado que sigue respondiendo mucho después SÍ
	// se bloquea. Finish NO se gatea: el alumno siempre puede enviar lo que ya
	// tenga respondido (un corte de luz al final no debe perder el examen).
	if att.DurationMinutes > 0 {
		limit := att.StartedAt.Add(time.Duration(att.DurationMinutes)*time.Minute + answerGrace)
		if time.Now().UTC().After(limit) {
			return domain.ErrTimeLimitExceeded
		}
	}

	// Resolver el set de option ids a persistir segun el shape del input
	// que mando el caller (back-compat con clientes que solo mandan
	// OptionID singular).
	optionIDs := in.OptionIDs
	if len(optionIDs) == 0 && in.OptionID != "" {
		optionIDs = []domain.OptionID{in.OptionID}
	}

	// Si hay options (path SINGLE/TF/SCALE/MULTI), validar inclusion en
	// el set de opciones de la pregunta.
	if len(optionIDs) > 0 {
		opts, err := h.options.ListByQuestion(ctx, in.QuestionID)
		if err != nil {
			return err
		}
		validIDs := make(map[domain.OptionID]struct{}, len(opts))
		for _, o := range opts {
			validIDs[o.ID] = struct{}{}
		}
		for _, oid := range optionIDs {
			if _, ok := validIDs[oid]; !ok {
				return domain.ErrAnswerOptionMismatch
			}
		}
	}

	// OPEN_TEXT: si no hay options ni answer_text, el alumno esta
	// "des-respondiendo" la pregunta. Aceptamos — ReplaceAnswer hace
	// DELETE sin INSERT y queda en blanco.
	return h.attempts.ReplaceAnswer(ctx, att.ID, in.QuestionID, optionIDs, in.AnswerText)
}

func (h *AttemptHandler) Finish(ctx context.Context, id domain.AttemptID) (*domain.ExamAttempt, error) {
	att, err := h.attempts.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if att.SubmittedAt != nil {
		return att, nil // idempotente: ya estaba finalizado
	}

	// Cargar exam para saber el exam_type. El scoring academico (sumar
	// is_correct) solo aplica a tests con concepto de "respuesta correcta"
	// — simulacro y habitos. Para vocacional las opciones son preferencias
	// RIASEC sin correcta/incorrecta; aplicarles la formula academica
	// produce scores aleatorios sin sentido (Bug B). El reporte vocacional
	// real lo hace analytics_service.GetReporteEstudiante agrupando por
	// categoria R/I/A/S/E/C.
	e, err := h.exams.FindByID(ctx, att.ExamID)
	if err != nil {
		return nil, err
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

	// maxScore = mejor caso (todas correctas) = Σ points por correcta.
	var maxScore float64
	for _, eq := range eqs {
		maxScore += eq.Points
	}

	var score float64
	// Vocacional (1) y Estilos/hábitos (3) NO se puntúan con is_correct: sus
	// preguntas son SCALE/Likert sin respuesta correcta. score=0, max_score=0
	// dejan claro que no aplica fórmula académica Y los excluyen de los
	// promedios de analytics (que filtran por max_score>0). Bug: antes solo se
	// zereaba vocacional → estilos entraba a los avg con max_score=Σpoints>0 y
	// score=0, contaminando con 0% falsos. El simulacro (2) sí se puntúa.
	// Cada tipo tiene su lectura dedicada (GetReporteEstudiante: RIASEC / EDA).
	const examTypeVocacional = int32(1)
	const examTypeHabitos = int32(3)
	if e.ExamTypeID == examTypeVocacional || e.ExamTypeID == examTypeHabitos {
		maxScore = 0
	} else {
		// Agrupamos respuestas por question — necesario para MULTIPLE_CHOICE
		// donde el alumno puede marcar N opciones para la misma pregunta.
		answersByQuestion := make(map[domain.QuestionID][]domain.AttemptAnswer, len(eqs))
		for _, a := range answers {
			answersByQuestion[a.QuestionID] = append(answersByQuestion[a.QuestionID], a)
		}

		// Scoring fiel a prod (migration 015): iteramos TODAS las preguntas del
		// exam (no solo las respondidas) porque el EN BLANCO también puntúa
		// (UCSP: blanco +1). Por pregunta: correcta → Points, incorrecta →
		// PointsIncorrect, en blanco → PointsBlank.
		for _, eq := range eqs {
			qid := eq.QuestionID
			q, err := h.questions.FindByID(ctx, qid)
			if err != nil {
				return nil, err
			}
			// SCALE/OpenText no tienen scoring académico (Likert / corrección
			// manual); tampoco aplica "en blanco".
			if q.Kind == domain.QuestionKindScale || q.Kind == domain.QuestionKindOpenText {
				continue
			}
			ansRows := answersByQuestion[qid]
			markedAny := false
			for _, a := range ansRows {
				if a.OptionID != "" {
					markedAny = true
					break
				}
			}
			if !markedAny {
				score += eq.PointsBlank // EN BLANCO
				continue
			}
			opts, err := h.options.ListByQuestion(ctx, qid)
			if err != nil {
				return nil, err
			}
			correct := false
			switch q.Kind {
			case domain.QuestionKindMultipleChoice:
				correctSet := make(map[domain.OptionID]struct{})
				for _, o := range opts {
					if o.IsCorrect {
						correctSet[o.ID] = struct{}{}
					}
				}
				markedSet := make(map[domain.OptionID]struct{}, len(ansRows))
				for _, a := range ansRows {
					if a.OptionID != "" {
						markedSet[a.OptionID] = struct{}{}
					}
				}
				if len(correctSet) > 0 && len(correctSet) == len(markedSet) {
					correct = true
					for id := range correctSet {
						if _, present := markedSet[id]; !present {
							correct = false
							break
						}
					}
				}
			default:
				// SINGLE_CHOICE / TRUE_FALSE / kind vacío → 1ra opción marcada.
				marked := ansRows[0].OptionID
				for _, o := range opts {
					if o.ID == marked && o.IsCorrect {
						correct = true
						break
					}
				}
			}
			if correct {
				score += eq.Points
			} else {
				score += eq.PointsIncorrect
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

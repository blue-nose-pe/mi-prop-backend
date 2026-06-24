package domain

import "time"

// Value objects tipados (UUIDs transportados como string).
type (
	ExamID     string
	QuestionID string
	OptionID   string
	AttemptID  string
	UserID     string // ref lógica a db_users.users.id
	SchoolID   string // ref lógica a db_users.school.id
	KeyID      string // ref lógica a db_keys.key.id
)

// ExamType — el code es la clave estable: vocacional, simulacro, habitos.
type ExamType struct {
	ID        int32
	Code      string
	Name      string
	Active    bool
	CreatedAt time.Time
	UpdatedAt *time.Time
}

// Exam representa una versión concreta de un examen. Una edición de
// preguntas crea un nuevo Exam con ParentExamID apuntando al original.
// Los attempts viejos siguen referenciando al exam original — histórico
// preservado por construcción.
type Exam struct {
	ID              ExamID
	ExamTypeID      int32
	SchoolID        SchoolID // "" = exam abierto, no atado a colegio
	ParentExamID    ExamID   // "" = primera versión
	Version         int32
	Code            string // identificador legible (unico). Autogen si el cliente no lo manda.
	Name            string
	StartAt         time.Time
	EndAt           time.Time
	MaxParticipants int32 // 0 = ilimitado
	// Reglas de puntaje por defecto del examen (migration 017): el builder
	// las copia a cada exam_question al agregarla. UCSP 5/0/1, Nacional
	// 1.25/0/0. El scoring real sigue siendo por pregunta.
	DefaultPoints          float64
	DefaultPointsIncorrect float64
	DefaultPointsBlank     float64
	Published              bool
	Active                 bool
	CreatedAt              time.Time
	UpdatedAt              *time.Time
}

// QuestionKind discrimina cómo el front renderiza la pregunta y cómo el
// reporting interpreta el option_sort_order.
//
//   - QuestionKindSingleChoice    — N opciones, 1 is_correct. Score binario.
//   - QuestionKindTrueFalse       — 2 opciones (V/F). Mismo scoring binario.
//   - QuestionKindScale           — N opciones "1..N" con sort_order=peso
//     Likert. Sin is_correct → no contribuye al score, pero reporting puede
//     promediarlo por question_id.
//   - QuestionKindMultipleChoice  — N opciones, M de ellas is_correct.
//     attempt_answer permite multi-fila por (attempt, question, option) via
//     PK extendido (migration 013/014). Scoring: full points si el alumno
//     marca EXACTAMENTE las correctas (set match); 0 en caso contrario.
//   - QuestionKindOpenText        — sin opciones; answer_text NVARCHAR(MAX)
//     guarda la respuesta libre. Scoring=0 automatico (el especialista
//     corrige manualmente despues).
type QuestionKind string

const (
	QuestionKindSingleChoice   QuestionKind = "SINGLE_CHOICE"
	QuestionKindTrueFalse      QuestionKind = "TRUE_FALSE"
	QuestionKindScale          QuestionKind = "SCALE"
	QuestionKindMultipleChoice QuestionKind = "MULTIPLE_CHOICE"
	QuestionKindOpenText       QuestionKind = "OPEN_TEXT"
)

// ValidKind retorna true si v es un kind soportado. Vacío también vale (se
// interpreta como SINGLE_CHOICE para back-compat con requests viejos).
func ValidKind(v string) bool {
	switch QuestionKind(v) {
	case "", QuestionKindSingleChoice, QuestionKindTrueFalse, QuestionKindScale,
		QuestionKindMultipleChoice, QuestionKindOpenText:
		return true
	}
	return false
}

// NormalizeKind aplica el default SINGLE_CHOICE a kinds vacíos. Asume que
// el caller ya validó con ValidKind.
func NormalizeKind(v string) QuestionKind {
	if v == "" {
		return QuestionKindSingleChoice
	}
	return QuestionKind(v)
}

// Question vive en el banco (independiente del exam).
type Question struct {
	ID        QuestionID
	Text      string
	Category  string // útil para Vocacional (matemática, lengua, ...)
	Kind      QuestionKind
	Active    bool
	CreatedAt time.Time
	UpdatedAt *time.Time
}

type QuestionOption struct {
	ID         OptionID
	QuestionID QuestionID
	Text       string
	IsCorrect  bool
	SortOrder  int32
}

// ExamQuestion — asociación N:M entre Exam y Question.
type ExamQuestion struct {
	ExamID     ExamID
	QuestionID QuestionID
	Points     float64 // puntaje por respuesta CORRECTA (decimal: Nacional usa 1.25)
	// PointsIncorrect/PointsBlank — scoring fiel a prod (migration 015). UCSP:
	// incorrecta 0, EN BLANCO +1. Nacional: correcta 1.25, incorrecta 0,
	// blanco 0. Decimal (migration 016) para representar el 1.25 fielmente.
	PointsIncorrect float64
	PointsBlank     float64
	SortOrder       int32
}

// ExamAttempt — sesión de un user contra un exam.
// Score y MaxScore se calculan al FinishAttempt.
type ExamAttempt struct {
	ID          AttemptID
	ExamID      ExamID
	UserID      UserID
	KeyID       KeyID // "" si no se requiere key (admin)
	Score       *float64
	MaxScore    *float64
	StartedAt   time.Time
	SubmittedAt *time.Time
	// DurationMinutes: límite de tiempo del intento, LOCKEADO al iniciar
	// (denormalizado desde el exam según tipo/código). 0 = sin límite (voca/
	// estilos). El server rechaza respuestas que llegan pasado este límite
	// (+ gracia) — el timer deja de ser solo del navegador.
	DurationMinutes int32
}

// AttemptAnswer: respuesta de un attempt a una pregunta.
type AttemptAnswer struct {
	AttemptID  AttemptID
	QuestionID QuestionID
	OptionID   OptionID
	AnsweredAt time.Time
}

// EnrichedAnswer hidrata una respuesta con metadata de la pregunta y la
// opcion elegida. Diseñado para reporting: evita el N+1 del consumer
// (sino tendria que hacer GetQuestion + GetOption por cada answer).
type EnrichedAnswer struct {
	QuestionID       QuestionID
	QuestionText     string
	QuestionCategory string // p.ej. R/I/A/S/E/C para vocacional (RIASEC)
	QuestionKind     QuestionKind
	OptionID         OptionID
	OptionText       string
	OptionSortOrder  int32 // usable como peso Likert (Nada=0..Mucho=3)
	OptionIsCorrect  bool
	AnsweredAt       time.Time
}

// IsOpen retorna true si el exam acepta nuevos attempts (publicado,
// activo y dentro de la ventana temporal).
func (e Exam) IsOpen(now time.Time) bool {
	if !e.Active || !e.Published {
		return false
	}
	return !now.Before(e.StartAt) && !now.After(e.EndAt)
}

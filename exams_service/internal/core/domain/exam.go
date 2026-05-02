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
	Name            string
	StartAt         time.Time
	EndAt           time.Time
	MaxParticipants int32 // 0 = ilimitado
	Published       bool
	Active          bool
	CreatedAt       time.Time
	UpdatedAt       *time.Time
}

// Question vive en el banco (independiente del exam).
type Question struct {
	ID        QuestionID
	Text      string
	Category  string // útil para Vocacional (matemática, lengua, ...)
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
	Points     int32
	SortOrder  int32
}

// ExamAttempt — sesión de un user contra un exam.
// Score y MaxScore se calculan al FinishAttempt.
type ExamAttempt struct {
	ID          AttemptID
	ExamID      ExamID
	UserID      UserID
	KeyID       KeyID // "" si no se requiere key (admin)
	Score       *int32
	MaxScore    *int32
	StartedAt   time.Time
	SubmittedAt *time.Time
}

// AttemptAnswer: respuesta de un attempt a una pregunta.
type AttemptAnswer struct {
	AttemptID  AttemptID
	QuestionID QuestionID
	OptionID   OptionID
	AnsweredAt time.Time
}

// IsOpen retorna true si el exam acepta nuevos attempts (publicado,
// activo y dentro de la ventana temporal).
func (e Exam) IsOpen(now time.Time) bool {
	if !e.Active || !e.Published {
		return false
	}
	return !now.Before(e.StartAt) && !now.After(e.EndAt)
}

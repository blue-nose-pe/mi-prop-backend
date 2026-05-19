package ports

import (
	"context"
	"time"

	"exams_service/internal/core/domain"
	"exams_service/internal/shared/search"
)

// =============== EXAM ===============

type ExamCommands interface {
	Create(ctx context.Context, in CreateExamInput) (*domain.Exam, error)
	Update(ctx context.Context, in UpdateExamInput) (*domain.Exam, error)
	Publish(ctx context.Context, id domain.ExamID) error
	Deactivate(ctx context.Context, id domain.ExamID) error
	// Reactivate vuelve un exam a active=true. Idempotente. No revive
	// estado de publicacion: si el exam estaba published al desactivarse,
	// queda published; si estaba draft, queda draft.
	Reactivate(ctx context.Context, id domain.ExamID) error
	// Clone crea una nueva versión del exam (inmutable después de publicar).
	// Copia todas sus exam_question. Devuelve el nuevo ExamID.
	Clone(ctx context.Context, id domain.ExamID) (*domain.Exam, error)
}

type ExamQueries interface {
	Get(ctx context.Context, id domain.ExamID) (*domain.Exam, error)
	Search(ctx context.Context, req search.Request) (*search.Response, error)
}

type CreateExamInput struct {
	ExamTypeCode    string // "vocacional" | "simulacro" | "habitos"
	SchoolID        domain.SchoolID
	Code            string // opcional. Si llega vacio, el handler autogenera.
	Name            string
	StartAt         time.Time
	EndAt           time.Time
	MaxParticipants int32
}

type UpdateExamInput struct {
	ID              domain.ExamID
	Code            string // opcional. Si llega vacio, el codigo actual no cambia.
	Name            string
	StartAt         time.Time
	EndAt           time.Time
	MaxParticipants int32
}

// =============== QUESTION (banco) ===============

type QuestionCommands interface {
	Create(ctx context.Context, in CreateQuestionInput) (*domain.Question, error)
	Update(ctx context.Context, in UpdateQuestionInput) (*domain.Question, error)
	Deactivate(ctx context.Context, id domain.QuestionID) error
	AddOption(ctx context.Context, in AddOptionInput) (*domain.QuestionOption, error)
	UpdateOption(ctx context.Context, in UpdateOptionInput) error
	RemoveOption(ctx context.Context, id domain.OptionID) error
}

type QuestionQueries interface {
	Get(ctx context.Context, id domain.QuestionID) (*domain.Question, error)
	ListOptions(ctx context.Context, qID domain.QuestionID) ([]domain.QuestionOption, error)
	Search(ctx context.Context, req search.Request) (*search.Response, error)
}

type CreateQuestionInput struct {
	Text     string
	Category string
}

type UpdateQuestionInput struct {
	ID       domain.QuestionID
	Text     string
	Category string
}

type AddOptionInput struct {
	QuestionID domain.QuestionID
	Text       string
	IsCorrect  bool
	SortOrder  int32
}

type UpdateOptionInput struct {
	ID        domain.OptionID
	Text      string
	IsCorrect bool
	SortOrder int32
}

// =============== EXAM ↔ QUESTION ===============

type ExamQuestionCommands interface {
	Add(ctx context.Context, in AddExamQuestionInput) error
	Remove(ctx context.Context, examID domain.ExamID, questionID domain.QuestionID) error
	Reorder(ctx context.Context, examID domain.ExamID, ordered []domain.QuestionID) error
}

type ExamQuestionQueries interface {
	List(ctx context.Context, examID domain.ExamID) ([]domain.ExamQuestion, error)
}

type AddExamQuestionInput struct {
	ExamID     domain.ExamID
	QuestionID domain.QuestionID
	Points     int32
	SortOrder  int32
}

// =============== ATTEMPT ===============

type AttemptCommands interface {
	Start(ctx context.Context, in StartAttemptInput) (*domain.ExamAttempt, error)
	Answer(ctx context.Context, in AnswerInput) error
	Finish(ctx context.Context, id domain.AttemptID) (*domain.ExamAttempt, error)
}

type AttemptQueries interface {
	Get(ctx context.Context, id domain.AttemptID) (*domain.ExamAttempt, error)
	ListByUser(ctx context.Context, userID domain.UserID) ([]domain.ExamAttempt, error)
	ListByExam(ctx context.Context, examID domain.ExamID) ([]domain.ExamAttempt, error)
	ListByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.ExamAttempt, error)
	ListByKey(ctx context.Context, keyID domain.KeyID) ([]domain.ExamAttempt, error)
	ListEnrichedAnswers(ctx context.Context, attemptID domain.AttemptID) ([]domain.EnrichedAnswer, error)
}

type StartAttemptInput struct {
	ExamID domain.ExamID
	UserID domain.UserID
	KeyID  domain.KeyID // "" si el caller es admin (sin requerir key)
}

type AnswerInput struct {
	AttemptID  domain.AttemptID
	QuestionID domain.QuestionID
	OptionID   domain.OptionID
}

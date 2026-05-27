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
	// Start crea (o reutiliza, si ya hay uno activo del mismo user para el
	// mismo exam) un attempt. El handler gRPC usa StartAttemptResult.Reused
	// para decidir si debe contar el uso en keys_service.
	Start(ctx context.Context, in StartAttemptInput) (*StartAttemptResult, error)
	Answer(ctx context.Context, in AnswerInput) error
	Finish(ctx context.Context, id domain.AttemptID) (*domain.ExamAttempt, error)
}

// StartAttemptResult: el attempt + flag indicando si era pre-existente.
// Cuando Reused=true, el caller NO debe consumir un nuevo uso de la key
// (porque el primer Start ya lo conto).
type StartAttemptResult struct {
	Attempt *domain.ExamAttempt
	Reused  bool
}

type AttemptQueries interface {
	Get(ctx context.Context, id domain.AttemptID) (*domain.ExamAttempt, error)
	// GetActiveByExamUser devuelve el attempt sin submit del user para
	// ese exam, o (nil, nil) si no hay ninguno. Lo usa el handler gRPC
	// StartAttempt para decidir si debe consumir un nuevo uso de la key
	// (no consumir si ya habia uno activo — fix Bug #2).
	GetActiveByExamUser(ctx context.Context, examID domain.ExamID, userID domain.UserID) (*domain.ExamAttempt, error)
	// CountSubmittedByKeyUser cuenta intentos finalizados del user PARA ESA
	// KEY especifica. Lo usa StartAttempt para chequear contra
	// key.MaxAttemptsPerUser antes de crear un nuevo attempt. Por (key,user)
	// — no global del exam.
	CountSubmittedByKeyUser(ctx context.Context, keyID domain.KeyID, userID domain.UserID) (int32, error)
	// GetMostRecentSubmittedByKeyUser devuelve el ultimo attempt SUBMITTED
	// del (key, user). Lo usa el RPC publico CountSubmittedByKeyUser.
	GetMostRecentSubmittedByKeyUser(ctx context.Context, keyID domain.KeyID, userID domain.UserID) (*domain.ExamAttempt, error)
	// GetMostRecentSubmittedByExamUser devuelve el ultimo attempt
	// finalizado del user (o nil si no hay ninguno). Sirve al front para
	// llevar al alumno a /resultados con su ultimo intento cuando ya
	// alcanzo el limite de intentos.
	GetMostRecentSubmittedByExamUser(ctx context.Context, examID domain.ExamID, userID domain.UserID) (*domain.ExamAttempt, error)
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
	// ExpectedExamTypeID: si !=0, Start exige que e.ExamTypeID == este valor.
	// Lo setea el handler gRPC cuando hay key, con el exam_type de la key,
	// para impedir que una key vocacional arranque un examen de simulacro
	// y viceversa (Fix A). Cuando es 0, no se hace el chequeo (modo admin).
	ExpectedExamTypeID int32
}

type AnswerInput struct {
	AttemptID  domain.AttemptID
	QuestionID domain.QuestionID
	OptionID   domain.OptionID
}

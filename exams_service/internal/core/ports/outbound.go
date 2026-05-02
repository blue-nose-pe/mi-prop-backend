package ports

import (
	"context"
	"time"

	"exams_service/internal/core/domain"
	"exams_service/internal/shared/search"
)

// =============== Repositories ===============

type ExamTypeRepository interface {
	FindByCode(ctx context.Context, code string) (*domain.ExamType, error)
	FindByID(ctx context.Context, id int32) (*domain.ExamType, error)
}

type ExamRepository interface {
	Save(ctx context.Context, e *domain.Exam) (domain.ExamID, error)
	Update(ctx context.Context, e *domain.Exam) error
	FindByID(ctx context.Context, id domain.ExamID) (*domain.Exam, error)
	SetActive(ctx context.Context, id domain.ExamID, active bool) error
	SetPublished(ctx context.Context, id domain.ExamID, published bool) error
	Search(ctx context.Context, req search.Request) (*search.Response, error)
}

type QuestionRepository interface {
	Save(ctx context.Context, q *domain.Question) (domain.QuestionID, error)
	Update(ctx context.Context, q *domain.Question) error
	FindByID(ctx context.Context, id domain.QuestionID) (*domain.Question, error)
	SetActive(ctx context.Context, id domain.QuestionID, active bool) error
	Search(ctx context.Context, req search.Request) (*search.Response, error)
}

type QuestionOptionRepository interface {
	Save(ctx context.Context, o *domain.QuestionOption) (domain.OptionID, error)
	Update(ctx context.Context, o *domain.QuestionOption) error
	Delete(ctx context.Context, id domain.OptionID) error
	ListByQuestion(ctx context.Context, qID domain.QuestionID) ([]domain.QuestionOption, error)
}

type ExamQuestionRepository interface {
	Add(ctx context.Context, eq *domain.ExamQuestion) error
	Remove(ctx context.Context, examID domain.ExamID, questionID domain.QuestionID) error
	Reorder(ctx context.Context, examID domain.ExamID, ordered []domain.QuestionID) error
	List(ctx context.Context, examID domain.ExamID) ([]domain.ExamQuestion, error)
	// CloneInto copia todas las exam_question del exam origen al destino.
	CloneInto(ctx context.Context, src, dst domain.ExamID) error
}

type AttemptRepository interface {
	Save(ctx context.Context, a *domain.ExamAttempt) (domain.AttemptID, error)
	FindByID(ctx context.Context, id domain.AttemptID) (*domain.ExamAttempt, error)
	ListByUser(ctx context.Context, userID domain.UserID) ([]domain.ExamAttempt, error)
	ListByExam(ctx context.Context, examID domain.ExamID) ([]domain.ExamAttempt, error)
	UpsertAnswer(ctx context.Context, ans *domain.AttemptAnswer) error
	ListAnswers(ctx context.Context, attemptID domain.AttemptID) ([]domain.AttemptAnswer, error)
	Finish(ctx context.Context, id domain.AttemptID, score, maxScore int32, when time.Time) error
	CountActiveByExam(ctx context.Context, examID domain.ExamID) (int32, error)
}

// =============== Cross-service clients (gRPC) ===============

// KeysClient es la interfaz que el core consume para validar/usar keys.
// La implementación concreta vive en adapters/outbound/keysclient (gRPC
// hacia keys_service). Mantenerlo como puerto permite testear el core
// sin depender de la red.
type KeysClient interface {
	Validate(ctx context.Context, keyCode string, examTypeCode string) (*KeyValidation, error)
	IncrementUsage(ctx context.Context, keyID domain.KeyID, attemptID domain.AttemptID, userID domain.UserID) error
}

type KeyValidation struct {
	KeyID     domain.KeyID
	SchoolID  domain.SchoolID
	ExpiresAt *time.Time
	OK        bool
	Reason    string
}

// =============== Audit ===============

type AuditEvent struct {
	ActorUserID   domain.UserID
	Action        string
	EntityType    string
	EntityID      string
	PayloadJSON   string
	CorrelationID string
	IP            string
	Success       bool
	ErrorMessage  string
}

type AuditSink interface {
	Record(ctx context.Context, ev AuditEvent) error
}

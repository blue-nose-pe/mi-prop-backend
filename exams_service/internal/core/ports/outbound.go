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
	// MaxVersionInFamily devuelve la version mas alta dentro de la familia
	// (raiz + todos los descendientes via parent_exam_id) del exam dado.
	MaxVersionInFamily(ctx context.Context, id domain.ExamID) (int32, error)
	// ExistsByCode chequea si el codigo legible ya esta en uso. La unicidad
	// tambien la garantiza el indice en DB, pero esto evita el roundtrip de
	// error al autogenerar.
	ExistsByCode(ctx context.Context, code string) (bool, error)
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
	// ListByColegio lista los attempts cuyos users pertenecen al colegio
	// indicado (users.school_id = X). Cross-DB JOIN a db_users.users; ambas
	// DBs viven en la misma instancia mssql-server.
	ListByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.ExamAttempt, error)
	// ListByKey lista los attempts iniciados con la key indicada
	// (exam_attempt.key_id = X). Excluye attempts sin key (modo admin).
	ListByKey(ctx context.Context, keyID domain.KeyID) ([]domain.ExamAttempt, error)
	UpsertAnswer(ctx context.Context, ans *domain.AttemptAnswer) error
	ListAnswers(ctx context.Context, attemptID domain.AttemptID) ([]domain.AttemptAnswer, error)
	// ListEnrichedAnswers devuelve las respuestas del attempt con la pregunta
	// y la opcion elegida pre-resueltas en un solo viaje a la DB. Pensado
	// para reportes vocacionales que necesitan category + sort_order.
	ListEnrichedAnswers(ctx context.Context, attemptID domain.AttemptID) ([]domain.EnrichedAnswer, error)
	Finish(ctx context.Context, id domain.AttemptID, score, maxScore int32, when time.Time) error
	CountActiveByExam(ctx context.Context, examID domain.ExamID) (int32, error)
	// FindActiveByExamUser devuelve el attempt activo (submitted_at IS NULL)
	// mas reciente del user para ese exam, o (nil, nil) si no hay ninguno.
	// Lo usa Start para devolver idempotente cuando el alumno refresca o
	// reintenta — sin esto, cada refresh creaba un nuevo attempt + nuevo
	// uso de la key, lo que permitia que un solo alumno consumiera N usos
	// del aforo.
	FindActiveByExamUser(ctx context.Context, examID domain.ExamID, userID domain.UserID) (*domain.ExamAttempt, error)
	// CountSubmittedByKeyUser cuenta attempts SUBMITTED del user PARA ESA
	// KEY especifica. StartAttempt lo compara contra key.MaxAttemptsPerUser
	// antes de crear un nuevo attempt. El conteo es POR KEY — no global del
	// exam — para que un alumno que ya rindio con una key previa no quede
	// bloqueado al recibir una key nueva.
	CountSubmittedByKeyUser(ctx context.Context, keyID domain.KeyID, userID domain.UserID) (int32, error)
	// FindMostRecentSubmittedByKeyUser devuelve el ultimo attempt SUBMITTED
	// del (key, user), o nil si no hay. Lo usa el RPC publico
	// AttemptService.CountSubmittedByKeyUser para que el gateway pueda
	// redirigir al alumno a sus resultados previos cuando ya consumio
	// sus intentos contra esa key.
	FindMostRecentSubmittedByKeyUser(ctx context.Context, keyID domain.KeyID, userID domain.UserID) (*domain.ExamAttempt, error)
	// FindMostRecentSubmittedByExamUser devuelve el ultimo attempt
	// finalizado del user. Lo usa StartAttempt para retornar el "ya
	// rendiste" con su attempt previo en lugar de tirar error pelado.
	FindMostRecentSubmittedByExamUser(ctx context.Context, examID domain.ExamID, userID domain.UserID) (*domain.ExamAttempt, error)
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
	KeyID       domain.KeyID
	SchoolID    domain.SchoolID
	ExamTypeID  int32 // exam_type_id de la key; el handler StartAttempt lo
	                  // compara con e.ExamTypeID para impedir que una key
	                  // vocacional dispare un examen de simulacro y viceversa.
	ExpiresAt   *time.Time
	OK          bool
	Reason      string
	// MaxAttemptsPerUser: cuantas veces UN MISMO alumno puede rendir la
	// prueba ligada a esta key. Default 1. 0 = sin limite. StartAttempt
	// cuenta los attempts submitted del (user_id, exam_id) y rechaza con
	// MAX_ATTEMPTS_REACHED cuando llega al limite.
	MaxAttemptsPerUser int32
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

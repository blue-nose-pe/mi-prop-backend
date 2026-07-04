package ports

import (
	"context"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/shared/search"
)

type SurveyRepository interface {
	Save(ctx context.Context, s *domain.Survey) (domain.SurveyID, error)
	Update(ctx context.Context, s *domain.Survey) error
	FindByID(ctx context.Context, id domain.SurveyID) (*domain.Survey, error)
	FindByCodeVersion(ctx context.Context, code string, version int32) (*domain.Survey, error)
	// FindActivePublished devuelve la encuesta publicada+activa aplicable: si
	// keyID != "" y existe una atada a esa key, esa tiene prioridad; si no,
	// cae a la generica por trigger_kind (sin key). ErrSurveyNotFound si no hay.
	FindActivePublished(ctx context.Context, triggerKind, keyID string) (*domain.Survey, error)
	// AssignKeyToSurvey ata una key a esta encuesta (tabla survey_key). Una key
	// usa como mucho una encuesta (upsert por key_id); una encuesta puede servir
	// a muchas keys. Reemplaza el survey.key_id de valor unico.
	AssignKeyToSurvey(ctx context.Context, surveyID domain.SurveyID, keyID string) error
	// UnassignKeysFromSurvey desata TODAS las llaves de la encuesta (la vuelve
	// general por tipo). Implementa el "-"/"Todas" del builder.
	UnassignKeysFromSurvey(ctx context.Context, surveyID domain.SurveyID) error
	// HasKeys: ¿la encuesta está dirigida a al menos una llave? Se usa para
	// permitir publicar encuestas de trigger no-tipo (recurring/legacy) atadas
	// a una llave.
	HasKeys(ctx context.Context, surveyID domain.SurveyID) (bool, error)
	SetPublished(ctx context.Context, id domain.SurveyID, published bool) error
	SetActive(ctx context.Context, id domain.SurveyID, active bool) error
	Search(ctx context.Context, req search.Request) (*search.Response, error)
}

type QuestionRepository interface {
	Save(ctx context.Context, q *domain.Question) (domain.QuestionID, error)
	Update(ctx context.Context, q *domain.Question) error
	Delete(ctx context.Context, id domain.QuestionID) error
	FindByID(ctx context.Context, id domain.QuestionID) (*domain.Question, error)
	ListBySurvey(ctx context.Context, surveyID domain.SurveyID) ([]domain.Question, error)
}

type ResponseRepository interface {
	// Submit guarda response + answers en una transacción.
	Submit(ctx context.Context, r *domain.Response, answers []domain.Answer) error
	FindByID(ctx context.Context, id domain.ResponseID) (*domain.Response, error)
	// CountAndAnswers retorna respuestas agregadas para calcular métricas.
	GetMetricsRaw(ctx context.Context, surveyID domain.SurveyID) (*RawMetrics, error)
	// ExistsByUserAttempt: ¿este user ya envió response para este survey + attempt?
	ExistsByUserAttempt(ctx context.Context, userID domain.UserID, surveyID domain.SurveyID, attemptID domain.AttemptID) (bool, error)
	// ExistsByUserSurvey: ¿este user ya envió cualquier response para este survey?
	ExistsByUserSurvey(ctx context.Context, userID domain.UserID, surveyID domain.SurveyID) (bool, error)
	// ExistsBySurveyAttempt: ¿ya hay una respuesta para este survey + intento?
	// Dedup del flujo ANÓNIMO (sin user_id): el intento identifica la sesión.
	ExistsBySurveyAttempt(ctx context.Context, surveyID domain.SurveyID, attemptID domain.AttemptID) (bool, error)
}

// RawMetrics es la cosecha cruda que el adapter devuelve; el handler
// (query) la procesa para calcular Average / NPS / Distribution.
type RawMetrics struct {
	TotalResponses int32
	Answers        []domain.Answer
	Questions      []domain.Question
}

// Audit ----

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

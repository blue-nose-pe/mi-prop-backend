package ports

import (
	"context"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/shared/search"
)

// =============== SURVEY ===============

type SurveyCommands interface {
	Create(ctx context.Context, in CreateSurveyInput) (*domain.Survey, error)
	Update(ctx context.Context, in UpdateSurveyInput) (*domain.Survey, error)
	Publish(ctx context.Context, id domain.SurveyID) error
	Deactivate(ctx context.Context, id domain.SurveyID) error
	// Reactivate vuelve un survey pausado a draft (active=true, published=false).
	Reactivate(ctx context.Context, id domain.SurveyID) (*domain.Survey, error)
	// Clone crea un draft nuevo con title="<orig> (copia)", code suffixeado
	// y mismo set de preguntas. Util cuando el survey original ya esta
	// published y no permite mas ediciones.
	Clone(ctx context.Context, id domain.SurveyID) (*domain.Survey, error)
	AddQuestion(ctx context.Context, in AddQuestionInput) (*domain.Question, error)
	UpdateQuestion(ctx context.Context, in UpdateQuestionInput) error
	RemoveQuestion(ctx context.Context, id domain.QuestionID) error
}

type SurveyQueries interface {
	Get(ctx context.Context, id domain.SurveyID) (*domain.Survey, error)
	GetByCode(ctx context.Context, code string, version int32) (*domain.Survey, error)
	ListQuestions(ctx context.Context, surveyID domain.SurveyID) ([]domain.Question, error)
	Search(ctx context.Context, req search.Request) (*search.Response, error)
}

// =============== RESPONSE ===============

type ResponseCommands interface {
	Submit(ctx context.Context, in SubmitResponseInput) (*domain.Response, error)
}

type ResponseQueries interface {
	Get(ctx context.Context, id domain.ResponseID) (*domain.Response, error)
	GetMetrics(ctx context.Context, surveyID domain.SurveyID) (*domain.Metrics, error)
}

// =============== DTOs ===============

type CreateSurveyInput struct {
	Code        string
	Title       string
	Description string
	TargetRole  string
	Trigger     string
}

type UpdateSurveyInput struct {
	ID          domain.SurveyID
	Title       string
	Description string
}

type AddQuestionInput struct {
	SurveyID    domain.SurveyID
	Text        string
	Kind        domain.QuestionKind
	SortOrder   int32
	OptionsJSON string
	Required    bool
}

type UpdateQuestionInput struct {
	ID          domain.QuestionID
	Text        string
	SortOrder   int32
	OptionsJSON string
	Required    bool
}

type SubmitResponseInput struct {
	SurveyID      domain.SurveyID
	UserID        domain.UserID
	ExamAttemptID domain.AttemptID
	Answers       []SubmittedAnswer
}

type SubmittedAnswer struct {
	QuestionID  domain.QuestionID
	ValueText   string
	ValueNumber *int32
}

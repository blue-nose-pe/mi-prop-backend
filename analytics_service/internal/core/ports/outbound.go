package ports

import (
	"context"
	"time"

	"analytics_service/internal/core/domain"
)

// =============== Upstream gRPC clients ===============
//
// Cada uno abstrae un servicio remoto. Los implementa adapters/outbound/clients/.
// El core consume estas interfaces para componer dashboards sin saber gRPC.

type UsersClient interface {
	GetUser(ctx context.Context, id domain.UserID) (*UpstreamUser, error)
	GetSchool(ctx context.Context, id domain.SchoolID) (*UpstreamSchool, error)
	ListSchools(ctx context.Context, activeOnly bool) ([]UpstreamSchool, error)
	ListAssignedColegios(ctx context.Context, asesorID domain.UserID) ([]UpstreamSchool, error)
	ListEstudiantesEnColegio(ctx context.Context, schoolID domain.SchoolID) ([]UpstreamUser, error)
}

type ExamsClient interface {
	ListAttemptsByUser(ctx context.Context, userID domain.UserID) ([]UpstreamAttempt, error)
	ListAttemptsByExam(ctx context.Context, examID domain.ExamID) ([]UpstreamAttempt, error)
	ListAttemptsByColegio(ctx context.Context, schoolID domain.SchoolID) ([]UpstreamAttempt, error)
	GetExam(ctx context.Context, id domain.ExamID) (*UpstreamExam, error)
	GetAttempt(ctx context.Context, id domain.AttemptID) (*UpstreamAttempt, error)
	ListEnrichedAnswers(ctx context.Context, attemptID domain.AttemptID) ([]UpstreamEnrichedAnswer, error)
}

type UpstreamEnrichedAnswer struct {
	QuestionID       domain.QuestionID
	QuestionText     string
	QuestionCategory string
	OptionID         string
	OptionText       string
	OptionSortOrder  int32
	OptionIsCorrect  bool
	AnsweredAt       time.Time
}

type KeysClient interface {
	ListKeysByAsesor(ctx context.Context, asesorID domain.UserID) ([]UpstreamKey, error)
	ListKeysByColegio(ctx context.Context, schoolID domain.SchoolID) ([]UpstreamKey, error)
}

// ----- DTOs upstream (separados de domain.* para no acoplarlos a gRPC) -----

type UpstreamUser struct {
	ID             domain.UserID
	Email          string
	FirstName      string
	LastName       string
	DocumentNumber string
	SchoolID       domain.SchoolID
	Active         bool
}

type UpstreamSchool struct {
	ID       domain.SchoolID
	Name     string
	City     string
	Category string
	Active   bool
}

type UpstreamAttempt struct {
	ID           domain.AttemptID
	ExamID       domain.ExamID
	UserID       domain.UserID
	Score        *int32
	MaxScore     *int32
	StartedAt    time.Time
	SubmittedAt  *time.Time
}

type UpstreamExam struct {
	ID           domain.ExamID
	ExamTypeCode string
	Name         string
	SchoolID     domain.SchoolID
	Version      int32
}

type UpstreamKey struct {
	ID             string
	Code           string
	ExamTypeCode   string
	SchoolID       domain.SchoolID
	AsesorUserID   domain.UserID
	CurrentUses    int32
	MaxUses        int32
}

// =============== Cache ===============

type Cache interface {
	Get(ctx context.Context, key string, dst any) (bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

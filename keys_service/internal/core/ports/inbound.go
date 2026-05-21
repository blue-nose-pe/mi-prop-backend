package ports

import (
	"context"
	"time"

	"keys_service/internal/core/domain"
	"keys_service/internal/shared/search"
)

type KeyCommands interface {
	Generate(ctx context.Context, in GenerateKeyInput) (*domain.Key, error)
	Update(ctx context.Context, in UpdateKeyInput) (*domain.Key, error)
	Deactivate(ctx context.Context, id domain.KeyID) error
	// Validate retorna la key si es usable en este momento. NO incrementa
	// el contador (eso lo hace exams_service vía IncrementUsage tras
	// crear el attempt). Patrón "validate then commit".
	Validate(ctx context.Context, code string) (*domain.Key, error)
	// IncrementUsage atómico + log key_usage. Llamado por exams_service
	// después de crear un attempt válido.
	IncrementUsage(ctx context.Context, in IncrementUsageInput) error
}

type KeyQueries interface {
	Get(ctx context.Context, id domain.KeyID) (*domain.Key, error)
	GetByCode(ctx context.Context, code string) (*domain.Key, error)
	ListByAsesor(ctx context.Context, asesorID domain.UserID) ([]domain.Key, error)
	ListByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.Key, error)
	ListUserIDsByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.UserID, error)
	Search(ctx context.Context, req search.Request) (*search.Response, error)
}

type GenerateKeyInput struct {
	Code         string // si vacío, el handler genera uno aleatorio
	ExamTypeID   int32
	SchoolID     domain.SchoolID
	AsesorUserID domain.UserID
	Mode         domain.KeyMode
	Grade        string
	Section      string
	ValidFrom    *time.Time
	ValidTo      *time.Time
	MaxUses      int32
}

type UpdateKeyInput struct {
	ID        domain.KeyID
	Grade     string
	Section   string
	ValidFrom *time.Time
	ValidTo   *time.Time
	// MaxUses es puntero para distinguir "no provisto" (nil) de "0 =
	// ilimitado" (valor explicito). Antes era int32 zero-value y un PATCH
	// que solo tocaba grade convertia la key en aforo infinito.
	MaxUses *int32
}

type IncrementUsageInput struct {
	KeyID     domain.KeyID
	UserID    domain.UserID
	AttemptID string
}

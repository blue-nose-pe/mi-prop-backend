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
	// UserHasKeyUsage: true si (key, user) ya tiene fila en key_usage.
	// El gateway lo usa para decidir si un user puede entrar a una key
	// con aforo lleno: si ya tiene plaza (true) es retry permitido; si
	// no la tiene (false) → aforo lleno, bloquear pre-OTP.
	UserHasKeyUsage(ctx context.Context, keyID domain.KeyID, userID domain.UserID) (bool, error)
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
	// ExamID opcional. Si !="" la key servira esa version puntual del exam
	// (deterministico). Si "" cae al fallback legacy (front busca "primer
	// exam publicado del exam_type_id") — comportamiento pre-v0.16.
	ExamID string
	// MaxAttemptsPerUser: cuantas veces UN MISMO alumno puede rendir la
	// prueba. 0 → server normaliza a 1 en Generate. Default 1.
	MaxAttemptsPerUser int32
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
	// MaxAttemptsPerUser tambien puntero: nil = no tocar; *N = nuevo valor.
	MaxAttemptsPerUser *int32
}

type IncrementUsageInput struct {
	KeyID     domain.KeyID
	UserID    domain.UserID
	AttemptID string
}

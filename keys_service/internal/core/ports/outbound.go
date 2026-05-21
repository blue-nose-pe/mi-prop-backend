package ports

import (
	"context"

	"keys_service/internal/core/domain"
	"keys_service/internal/shared/search"
)

type KeyRepository interface {
	Save(ctx context.Context, k *domain.Key) (domain.KeyID, error)
	Update(ctx context.Context, k *domain.Key) error
	FindByID(ctx context.Context, id domain.KeyID) (*domain.Key, error)
	FindByCode(ctx context.Context, code string) (*domain.Key, error)
	SetActive(ctx context.Context, id domain.KeyID, active bool) error
	ListByAsesor(ctx context.Context, asesorID domain.UserID) ([]domain.Key, error)
	ListByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.Key, error)
	// ListUserIDsByColegio: distinct user_ids que usaron keys del colegio.
	// JOIN key_usage <-> [key] filtrando key.school_id = X. Lo usa el
	// gateway para hacer "Estudiantes de Colegio X" aditivo: incluir
	// alumnos que rindieron via keys del colegio aunque users.school_id
	// apunte a otro lado.
	ListUserIDsByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.UserID, error)
	Search(ctx context.Context, req search.Request) (*search.Response, error)
	// IncrementUses atómico — UPDATE ... SET current_uses = current_uses + 1
	// con WHERE que valida el límite. Devuelve filas afectadas.
	// 0 filas → la key no es usable (carrera concurrente o aforo lleno).
	IncrementUses(ctx context.Context, id domain.KeyID) (int64, error)
}

type KeyUsageRepository interface {
	Save(ctx context.Context, u *domain.KeyUsage) error
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

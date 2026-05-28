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
	// IncrementUses atomico — UPDATE ... SET current_uses = current_uses + 1
	// con WHERE que valida limite + NOT EXISTS de key_usage(key,user). Esto
	// hace que max_uses cuente USUARIOS DISTINTOS, no attempts totales.
	// 0 filas → o el user ya tenia key_usage (retry, permitido sin contar),
	// o la key no es usable. Disambiguar con KeyUsageRepository.ExistsByKeyUser.
	IncrementUses(ctx context.Context, id domain.KeyID, userID domain.UserID) (int64, error)
}

type KeyUsageRepository interface {
	Save(ctx context.Context, u *domain.KeyUsage) error
	// ExistsByKeyUser indica si el (key, user) ya tiene al menos un row de
	// key_usage. Usado por IncrementUsage para distinguir "retry permitido"
	// de "key no usable" cuando IncrementUses devuelve 0 filas.
	ExistsByKeyUser(ctx context.Context, keyID domain.KeyID, userID domain.UserID) (bool, error)
}

// HubspotSyncer — port para sincronizar el custom object Key al CRM.
// Paridad con v1: cada key generada/editada se replica como custom
// object en HubSpot con asociaciones a Asesor + Colegio. Best-effort:
// los handlers Generate/Update lo invocan en goroutine separada para
// no bloquear la respuesta del front si HubSpot esta lento o caido.
type HubspotSyncer interface {
	SyncKey(ctx context.Context, in HubspotSyncKeyInput) error
}

// HubspotSyncKeyInput — campos que hubspot_service necesita para
// upsertear el custom object Key. asesor_record_id y school_record_id
// son opcionales: si vienen, hubspot_service los usa directo; sino
// hace search por mi_proposito_asesor_id / mi_proposito___id_colegio.
type HubspotSyncKeyInput struct {
	Code           string
	ExamTypeCode   string // "vocacional" | "simulacro" | "habitos"
	AsesorUserID   domain.UserID
	SchoolID       domain.SchoolID
	SchoolName     string
	Grade          string
	Section        string
	ValidFrom      string // RFC 3339
	ValidTo        string // RFC 3339
	MaxUses        int32
	AsesorRecordID string
	SchoolRecordID string
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

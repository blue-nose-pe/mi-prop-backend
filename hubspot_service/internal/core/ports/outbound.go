package ports

import (
	"context"

	"hubspot_service/internal/core/domain"
)

// =============== HubSpot client ===============
//
// El core consume estos puertos sin conocer detalles HTTP (axios, http.Client,
// SDK oficial, etc). Lo implementa adapters/outbound/hubspot/.

type HubspotClient interface {
	// UpsertContactByDNI: busca por DNI, crea si no existe, actualiza si sí.
	UpsertContactByDNI(ctx context.Context, props map[string]string, dni string) (domain.RecordID, error)
	UpdateContact(ctx context.Context, recordID domain.RecordID, props map[string]string) error
	FindContactByDNI(ctx context.Context, dni string) (domain.RecordID, error)
	FindContactByEmail(ctx context.Context, email string) (domain.RecordID, error)

	UpsertCustomObjectByProp(ctx context.Context, typeID, keyProp, keyValue string, props map[string]string) (domain.RecordID, error)
}

// =============== OTP webhook trigger ===============

type OTPWebhook interface {
	Trigger(ctx context.Context, email, otp string) error
}

// =============== Cola de jobs ===============
//
// Producer / consumer separados — el server enqueue, el worker dequeue.
// Implementación: asynq sobre Redis con backoff exponencial.

type JobEnqueuer interface {
	EnqueueSyncResult(ctx context.Context, r domain.ExamResult) error
	EnqueueUpsertColegio(ctx context.Context, c domain.ColegioPayload) error
	EnqueueUpsertAsesor(ctx context.Context, a domain.AsesorPayload) error
}

type JobAdmin interface {
	ListFailed(ctx context.Context, limit, offset uint32) ([]domain.FailedSync, error)
	Retry(ctx context.Context, queue, jobID string) error
}

// =============== Persistencia opcional para hubspot_record_id ===============
//
// Cuando el cliente provee user_id en UpsertContact, llamamos de vuelta a
// users_service para que persista el record_id devuelto. Lo abstraemos
// detrás de un puerto — tests no necesitan red.

type UsersServiceCallback interface {
	SetUserHubspotRecordID(ctx context.Context, userID domain.UserID, recordID domain.RecordID) error
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

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
	// UpsertContactByEmail: misma semantica que ByDNI pero buscando por email.
	// Lo usa SendOTP para garantizar que el contacto exista (con la prop
	// otp_estudiante) antes de disparar el webhook, sino la Automation de
	// HubSpot no encuentra a quien mandarle el correo y el OTP nunca llega.
	UpsertContactByEmail(ctx context.Context, props map[string]string, email string) (domain.RecordID, error)
	// AssociateContactToCompany crea la relacion default Contact <-> Company
	// (object types 0-1 y 0-2). La usa SendOTP despues del upsert cuando
	// el caller provee el record_id del colegio (school.hubspot_record_id),
	// para que el contacto aparezca con la Company correspondiente en el
	// sidebar de HubSpot — mismo patron que P1 (createHubspotContacto.js).
	AssociateContactToCompany(ctx context.Context, contactID, companyID domain.RecordID) error
	UpdateContact(ctx context.Context, recordID domain.RecordID, props map[string]string) error
	FindContactByDNI(ctx context.Context, dni string) (domain.RecordID, error)
	FindContactByEmail(ctx context.Context, email string) (domain.RecordID, error)

	UpsertCustomObjectByProp(ctx context.Context, typeID, keyProp, keyValue string, props map[string]string) (domain.RecordID, error)

	// FindObjectByProp busca un object del typeID dado por (prop=value)
	// usando la search API. Devuelve "" si no existe. La usa SyncKey para
	// resolver record_ids de Asesor y Company cuando el caller no los
	// provee (lookup por mi_proposito_asesor_id / mi_proposito___id_colegio).
	FindObjectByProp(ctx context.Context, typeID, keyProp, keyValue string) (domain.RecordID, error)

	// AssociateObjects crea asociacion default entre dos objects de
	// cualquier typeID. Generaliza AssociateContactToCompany para soportar
	// Key->Asesor, Key->Company, etc. Idempotente: PUT en HubSpot ya
	// devuelve OK si la asociacion existia.
	AssociateObjects(ctx context.Context, fromTypeID string, fromID domain.RecordID, toTypeID string, toID domain.RecordID) error
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

package ports

import (
	"context"

	"hubspot_service/internal/core/domain"
)

// =============== Sync (write side) ===============
//
// UpsertContact es SÍNCRONO porque el caller (users_service) necesita
// el record_id devuelto. Los demás (results, schools, asesores) van
// por cola con backoff exponencial — el caller no espera.

type SyncCommands interface {
	UpsertContact(ctx context.Context, c domain.Contact) (domain.RecordID, error)
	SendOTP(ctx context.Context, in SendOTPInput) error
	SyncStudentContact(ctx context.Context, in SyncStudentContactInput) error
	// SyncLead upsertea el contacto de un lead de la landing masiva
	// ("Preparate") al portal UCSP, SIN asociar Company (lead anonimo, sin
	// colegio real). Setea origen_del_contacto="Examen Simulacro" + flags
	// de origen Mi Proposito. Best-effort.
	SyncLead(ctx context.Context, in SyncLeadInput) error
	// SyncKey upsertea el custom object Key (2-32450705) + asociaciones a
	// Asesor (2-32448565) y Company (0-2). Best-effort: errores de
	// asociacion no fallan el upsert. Paridad con v1.
	SyncKey(ctx context.Context, k domain.KeyPayload) error
	EnqueueExamResult(ctx context.Context, r domain.ExamResult) error
	EnqueueAsesor(ctx context.Context, a domain.AsesorPayload) error
	EnqueueColegio(ctx context.Context, c domain.ColegioPayload) error
}

// SyncStudentContactInput — payload para sincronizar el contacto del
// estudiante a HubSpot sin OTP. Reutilizado en el flujo "asesor crea
// estudiante" (POST /api/asesores/students). Mismo upsert+associate que
// SendOTP, sin disparar el webhook.
type SyncStudentContactInput struct {
	Email          string
	FirstName      string
	LastName       string
	DocumentNumber string
	Phone          string
	SchoolIntID    int32
	SchoolRecordID string
}

// SyncLeadInput — payload para sincronizar el contacto de un lead de la
// landing masiva. Solo Email es obligatorio. GraduationYear va como string
// (props ano_de_egreso / ano_colegio_abc_fb son texto en el portal).
type SyncLeadInput struct {
	Email          string
	FirstName      string
	LastName       string
	DocumentNumber string
	Phone          string
	GraduationYear string
}

type SendOTPInput struct {
	Email string
	OTP   string
	// Datos del estudiante. Opcionales en request-otp (login posterior,
	// el contacto ya existe en HubSpot con esa info); obligatorios desde
	// el front en register-with-key. Cuando vienen no-vacios la upsert
	// los persiste en el contacto para que no aparezca en blanco en CRM.
	FirstName      string
	LastName       string
	DocumentNumber string
	Phone          string
	SchoolID       string // UUID interno (no se manda a HubSpot)
	SchoolIntID    int32  // INT IDENTITY de school; mapea a mi_proposito___id_colegio
	SchoolRecordID string // record_id de la Company en HubSpot. Si viene
	                       // no-vacio, asociamos Contact <-> Company despues
	                       // del upsert (replica del P1).
}

// =============== Read side ===============

type SyncQueries interface {
	GetContactByDNI(ctx context.Context, dni string) (domain.RecordID, error)
	ListFailed(ctx context.Context, limit, offset uint32) ([]domain.FailedSync, error)
}

// =============== Admin ===============

type AdminCommands interface {
	RetryFailed(ctx context.Context, queue, jobID string) error
	TriggerFullSync(ctx context.Context) error
}

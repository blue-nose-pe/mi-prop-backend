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
	EnqueueExamResult(ctx context.Context, r domain.ExamResult) error
	EnqueueAsesor(ctx context.Context, a domain.AsesorPayload) error
	EnqueueColegio(ctx context.Context, c domain.ColegioPayload) error
}

type SendOTPInput struct {
	Email string
	OTP   string
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

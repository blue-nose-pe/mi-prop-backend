// Package command — write side. Orquesta llamadas a HubSpot, encolado
// de jobs y callback al users_service.
package command

import (
	"context"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
)

// SyncHandler implementa ports.SyncCommands.
//
// Reglas:
//   - UpsertContact es síncrono: el caller necesita el record_id.
//     Si el cliente provee user_id, intentamos persistirlo en users_service
//     (best-effort: si users_service no responde, el record_id ya quedó
//     creado en HubSpot, así que devolvemos OK al caller).
//   - SendOTP es síncrono: el alumno está esperando el email.
//   - Los demás (results, asesores, colegios) se encolan con backoff
//     exponencial (5 intentos, 1s/2s/4s/8s/16s) → DLQ.
type SyncHandler struct {
	hs       ports.HubspotClient
	otp      ports.OTPWebhook
	queue    ports.JobEnqueuer
	usersCB  ports.UsersServiceCallback // nullable — best-effort
}

var _ ports.SyncCommands = (*SyncHandler)(nil)

func NewSyncHandler(
	hs ports.HubspotClient,
	otp ports.OTPWebhook,
	queue ports.JobEnqueuer,
	usersCB ports.UsersServiceCallback,
) *SyncHandler {
	return &SyncHandler{hs: hs, otp: otp, queue: queue, usersCB: usersCB}
}

func (h *SyncHandler) UpsertContact(ctx context.Context, c domain.Contact) (domain.RecordID, error) {
	if c.DNI == "" {
		return "", domain.ErrInvalidPayload
	}
	recordID, err := h.hs.UpsertContactByDNI(ctx, c.ToProperties(), c.DNI)
	if err != nil {
		return "", err
	}
	if c.UserID != "" && h.usersCB != nil {
		// best-effort: si falla, lo logueamos arriba pero no rompemos al caller.
		_ = h.usersCB.SetUserHubspotRecordID(ctx, c.UserID, recordID)
	}
	return recordID, nil
}

func (h *SyncHandler) SendOTP(ctx context.Context, in ports.SendOTPInput) error {
	// 1) Garantizamos que el contacto exista con otp_estudiante seteado.
	//    Antes solo escribiamos la prop SI el contacto ya existia: para
	//    estudiantes nuevos (registro publico via key) el contacto no
	//    estaba en HubSpot todavia, la prop quedaba vacia y la Automation
	//    no encontraba el OTP -> el email nunca salia. Upsert lo arregla
	//    creando el contacto on-the-fly con la prop seteada.
	if _, err := h.hs.UpsertContactByEmail(ctx, map[string]string{"otp_estudiante": in.OTP}, in.Email); err != nil {
		// Best-effort: si HubSpot esta caido o rate-limited igual disparamos
		// el webhook con el OTP en el payload, por si la Automation lo lee
		// de ahi. No bloqueamos el flujo del estudiante.
		_ = err
	}
	// 2) Disparar webhook trigger (HubSpot Automation manda el email).
	return h.otp.Trigger(ctx, in.Email, in.OTP)
}

func (h *SyncHandler) EnqueueExamResult(ctx context.Context, r domain.ExamResult) error {
	if !r.ExamTypeCode.Valid() {
		return domain.ErrInvalidExamType
	}
	if r.DNI == "" || r.AttemptID == "" {
		return domain.ErrInvalidPayload
	}
	return h.queue.EnqueueSyncResult(ctx, r)
}

func (h *SyncHandler) EnqueueAsesor(ctx context.Context, a domain.AsesorPayload) error {
	if a.Email == "" {
		return domain.ErrInvalidPayload
	}
	return h.queue.EnqueueUpsertAsesor(ctx, a)
}

func (h *SyncHandler) EnqueueColegio(ctx context.Context, c domain.ColegioPayload) error {
	if c.Email == "" {
		return domain.ErrInvalidPayload
	}
	return h.queue.EnqueueUpsertColegio(ctx, c)
}

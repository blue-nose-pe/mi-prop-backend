// Package auditadapter conecta el interceptor genérico auditmw (que vive
// en internal/shared/) con la interfaz ports.AuditSink del core.
//
// Su única responsabilidad: convertir auditmw.Event → ports.AuditEvent y
// delegar al sink concreto (mssql.AuditSink).
package auditadapter

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/shared/auditmw"
)

// Bridge implementa auditmw.Sink delegando en un ports.AuditSink.
type Bridge struct {
	sink ports.AuditSink
}

func NewBridge(sink ports.AuditSink) *Bridge { return &Bridge{sink: sink} }

func (b *Bridge) Record(ctx context.Context, ev auditmw.Event) error {
	return b.sink.Record(ctx, ports.AuditEvent{
		ActorUserID:   domain.UserID(ev.ActorUserID),
		Action:        ev.Action,
		PayloadJSON:   ev.PayloadJSON,
		CorrelationID: ev.CorrelationID,
		IP:            ev.IP,
		Success:       ev.Success,
		ErrorMessage:  ev.ErrorMessage,
		// EntityType/EntityID/CreatedAt los rellena la BD (defaults) o se
		// agregan después si hace falta más granularidad por entidad.
	})
}

// Sanitizer ofusca campos sensibles antes de serializar el request al
// audit log. Hoy oculta password y tokens; ampliar cuando aparezcan otros.
//
// Patrón: en lugar de tocar el mensaje proto (riesgo de mutar el original),
// devolvemos una copia con los campos en cuestión vacíos.
//
// Mantenemos esto en el adapter (no en core) porque depende del concreto
// proto del servicio.

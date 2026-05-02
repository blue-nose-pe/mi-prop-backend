// Package auditadapter conecta el interceptor genérico auditmw con el
// puerto AuditSink del core (que el adapter mssql implementa).
package auditadapter

import (
	"context"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
	"exams_service/internal/shared/auditmw"
)

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
	})
}

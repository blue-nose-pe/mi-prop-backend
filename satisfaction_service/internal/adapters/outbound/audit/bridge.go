// Package auditadapter conecta auditmw con ports.AuditSink.
package auditadapter

import (
	"context"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
	"satisfaction_service/internal/shared/auditmw"
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

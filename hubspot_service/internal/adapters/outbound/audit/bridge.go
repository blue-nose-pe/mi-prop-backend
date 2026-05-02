// Package auditadapter conecta auditmw (interceptor genérico) con el
// puerto AuditSink del core. hubspot_service no persiste en SQL, así
// que aquí el bridge va a stdout estructurado (logs centralizados via
// Azure Monitor).
package auditadapter

import (
	"context"
	"encoding/json"
	"log"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
	"hubspot_service/internal/shared/auditmw"
)

// StdoutSink: escribe el evento como JSON line. Funciona perfecto con
// el log collector de AKS (Container Insights → Log Analytics workspace).
type StdoutSink struct{}

var _ ports.AuditSink = (*StdoutSink)(nil)

func NewStdoutSink() *StdoutSink { return &StdoutSink{} }

func (s *StdoutSink) Record(_ context.Context, ev ports.AuditEvent) error {
	b, _ := json.Marshal(ev)
	log.Printf("AUDIT %s", string(b))
	return nil
}

// Bridge: implementa auditmw.Sink delegando en ports.AuditSink.
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

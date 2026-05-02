package mssqladapter

import (
	"context"
	"database/sql"

	"exams_service/internal/core/ports"
)

type AuditSink struct {
	db *sql.DB
}

var _ ports.AuditSink = (*AuditSink)(nil)

func NewAuditSink(db *sql.DB) *AuditSink { return &AuditSink{db: db} }

func (s *AuditSink) Record(ctx context.Context, ev ports.AuditEvent) error {
	const q = `
		INSERT INTO audit_log (
		    actor_user_id, action, entity_type, entity_id, payload_json,
		    correlation_id, ip, success, error_message
		) VALUES (
		    IIF(@p1 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p1)),
		    @p2, NULLIF(@p3, ''), NULLIF(@p4, ''),
		    NULLIF(@p5, ''), NULLIF(@p6, ''), NULLIF(@p7, ''),
		    @p8, NULLIF(@p9, '')
		)`
	_, err := s.db.ExecContext(ctx, q,
		string(ev.ActorUserID),
		ev.Action, ev.EntityType, ev.EntityID, ev.PayloadJSON,
		ev.CorrelationID, ev.IP, ev.Success, ev.ErrorMessage,
	)
	return err
}

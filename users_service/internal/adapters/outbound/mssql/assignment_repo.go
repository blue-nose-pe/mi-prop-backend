package mssqladapter

import (
	"context"
	"database/sql"
	"errors"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

type AssignmentRepo struct {
	db *sql.DB
}

var _ ports.AssignmentRepository = (*AssignmentRepo)(nil)

func NewAssignmentRepo(db *sql.DB) *AssignmentRepo { return &AssignmentRepo{db: db} }

// Reassign cierra la asignación vigente (si existe) e inserta una nueva,
// todo en UNA SOLA transacción → preserva consistencia del histórico.
func (r *AssignmentRepo) Reassign(
	ctx context.Context,
	kind ports.AssignmentKind,
	source, target, by domain.UserID,
) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Cerrar la vigente (si existe). Idempotente: si no había, no falla.
	const closeQ = `
		UPDATE assignment
		   SET valid_to = SYSUTCDATETIME()
		 WHERE kind = @p1
		   AND target_user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		   AND valid_to IS NULL`
	if _, err := tx.ExecContext(ctx, closeQ, string(kind), string(target)); err != nil {
		return err
	}

	// 2. Insertar la nueva. Si source == "" o ID nil, no insertamos
	//    (significa "desasignar" → solo se cerró la vigente).
	if string(source) == "" {
		return tx.Commit()
	}

	const insertQ = `
		INSERT INTO assignment (kind, source_user_id, target_user_id, created_by)
		VALUES (@p1,
		        CONVERT(UNIQUEIDENTIFIER, @p2),
		        CONVERT(UNIQUEIDENTIFIER, @p3),
		        IIF(@p4 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p4)))`
	if _, err := tx.ExecContext(ctx, insertQ,
		string(kind), string(source), string(target), string(by),
	); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *AssignmentRepo) FindCurrent(
	ctx context.Context,
	kind ports.AssignmentKind,
	target domain.UserID,
) (*ports.AssignmentRecord, error) {
	const q = `
		SELECT TOP 1
		    CONVERT(NVARCHAR(36), id),
		    kind,
		    CONVERT(NVARCHAR(36), source_user_id),
		    CONVERT(NVARCHAR(36), target_user_id),
		    valid_from, valid_to,
		    ISNULL(CONVERT(NVARCHAR(36), created_by), ''),
		    created_at
		  FROM assignment
		 WHERE kind = @p1
		   AND target_user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		   AND valid_to IS NULL
		 ORDER BY valid_from DESC`

	var (
		rec       ports.AssignmentRecord
		kindStr   string
		sourceStr string
		targetStr string
		createdBy string
		validTo   sql.NullTime
	)
	err := r.db.QueryRowContext(ctx, q, string(kind), string(target)).Scan(
		&rec.ID, &kindStr, &sourceStr, &targetStr,
		&rec.ValidFrom, &validTo, &createdBy, &rec.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrAssignmentNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.Kind = ports.AssignmentKind(kindStr)
	rec.SourceUserID = domain.UserID(sourceStr)
	rec.TargetUserID = domain.UserID(targetStr)
	rec.CreatedBy = domain.UserID(createdBy)
	if validTo.Valid {
		t := validTo.Time
		rec.ValidTo = &t
	}
	return &rec, nil
}

func (r *AssignmentRepo) ListHistory(
	ctx context.Context,
	kind ports.AssignmentKind,
	target domain.UserID,
) ([]ports.AssignmentRecord, error) {
	const q = `
		SELECT CONVERT(NVARCHAR(36), id),
		       kind,
		       CONVERT(NVARCHAR(36), source_user_id),
		       CONVERT(NVARCHAR(36), target_user_id),
		       valid_from, valid_to,
		       ISNULL(CONVERT(NVARCHAR(36), created_by), ''),
		       created_at
		  FROM assignment
		 WHERE kind = @p1
		   AND target_user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		 ORDER BY valid_from DESC`
	rows, err := r.db.QueryContext(ctx, q, string(kind), string(target))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ports.AssignmentRecord
	for rows.Next() {
		var (
			rec       ports.AssignmentRecord
			kindStr   string
			sourceStr string
			targetStr string
			createdBy string
			validTo   sql.NullTime
		)
		if err := rows.Scan(
			&rec.ID, &kindStr, &sourceStr, &targetStr,
			&rec.ValidFrom, &validTo, &createdBy, &rec.CreatedAt,
		); err != nil {
			return nil, err
		}
		rec.Kind = ports.AssignmentKind(kindStr)
		rec.SourceUserID = domain.UserID(sourceStr)
		rec.TargetUserID = domain.UserID(targetStr)
		rec.CreatedBy = domain.UserID(createdBy)
		if validTo.Valid {
			t := validTo.Time
			rec.ValidTo = &t
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

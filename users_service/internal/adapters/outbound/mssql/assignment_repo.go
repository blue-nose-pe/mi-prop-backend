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

// AddSource permite VARIOS source vigentes por target (many-to-many). Cierra
// solo la vigente del PAR exacto (kind, source, target) — idempotente, evita
// duplicar el mismo coordinador — e inserta una nueva. NO toca otros sources
// del mismo target. Úsese para "varios coordinadores por colegio".
func (r *AssignmentRepo) AddSource(
	ctx context.Context,
	kind ports.AssignmentKind,
	source, target, by domain.UserID,
) error {
	if string(source) == "" {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	const closeQ = `
		UPDATE assignment
		   SET valid_to = SYSUTCDATETIME()
		 WHERE kind = @p1
		   AND source_user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		   AND target_user_id = CONVERT(UNIQUEIDENTIFIER, @p3)
		   AND valid_to IS NULL`
	if _, err := tx.ExecContext(ctx, closeQ, string(kind), string(source), string(target)); err != nil {
		return err
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

// RevokeSource cierra la vigente del par exacto (kind, source, target). No falla
// si no había (idempotente).
func (r *AssignmentRepo) RevokeSource(
	ctx context.Context,
	kind ports.AssignmentKind,
	source, target, by domain.UserID,
) error {
	const q = `
		UPDATE assignment
		   SET valid_to = SYSUTCDATETIME()
		 WHERE kind = @p1
		   AND source_user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		   AND target_user_id = CONVERT(UNIQUEIDENTIFIER, @p3)
		   AND valid_to IS NULL`
	_, err := r.db.ExecContext(ctx, q, string(kind), string(source), string(target))
	return err
}

// ListSourcesByTarget retorna los source_user_id con asignación vigente para
// (kind, target). Para "listar los coordinadores de un colegio".
func (r *AssignmentRepo) ListSourcesByTarget(
	ctx context.Context,
	kind ports.AssignmentKind,
	target domain.UserID,
) ([]domain.UserID, error) {
	const q = `
		SELECT DISTINCT CONVERT(NVARCHAR(36), source_user_id)
		  FROM assignment
		 WHERE kind = @p1
		   AND target_user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		   AND valid_to IS NULL`
	rows, err := r.db.QueryContext(ctx, q, string(kind), string(target))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.UserID, 0)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, domain.UserID(s))
	}
	return out, rows.Err()
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

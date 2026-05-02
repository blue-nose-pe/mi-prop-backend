// Package mssqladapter implementa los puertos outbound de keys_service
// contra Azure SQL.
package mssqladapter

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	mssql "github.com/microsoft/go-mssqldb"

	"keys_service/internal/core/domain"
	"keys_service/internal/core/ports"
)

type KeyRepo struct {
	db *sql.DB
	*SearchEngine
}

var _ ports.KeyRepository = (*KeyRepo)(nil)

func NewKeyRepo(db *sql.DB) *KeyRepo {
	return &KeyRepo{db: db, SearchEngine: NewSearchEngine(db, keySearchSchema)}
}

const keyCols = `CONVERT(NVARCHAR(36), id),
		code,
		exam_type_id,
		ISNULL(CONVERT(NVARCHAR(36), school_id), ''),
		CONVERT(NVARCHAR(36), asesor_user_id),
		mode,
		ISNULL(grade, ''),
		ISNULL(section, ''),
		valid_from,
		valid_to,
		max_uses,
		current_uses,
		active,
		created_at,
		updated_at`

func (r *KeyRepo) Save(ctx context.Context, k *domain.Key) (domain.KeyID, error) {
	const q = `
		INSERT INTO [key] (code, exam_type_id, school_id, asesor_user_id,
		                    mode, grade, section, valid_from, valid_to,
		                    max_uses, current_uses, active)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (@p1, @p2,
		        IIF(@p3 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p3)),
		        CONVERT(UNIQUEIDENTIFIER, @p4),
		        @p5, NULLIF(@p6, ''), NULLIF(@p7, ''),
		        @p8, @p9, @p10, 0, @p11)`
	var id string
	err := r.db.QueryRowContext(ctx, q,
		k.Code, k.ExamTypeID, string(k.SchoolID), string(k.AsesorUserID),
		string(k.Mode), k.Grade, k.Section,
		nullableTime(k.ValidFrom), nullableTime(k.ValidTo),
		k.MaxUses, k.Active,
	).Scan(&id)
	if err != nil {
		return "", mapDuplicate(err)
	}
	return domain.KeyID(id), nil
}

func (r *KeyRepo) Update(ctx context.Context, k *domain.Key) error {
	const q = `
		UPDATE [key]
		   SET grade      = NULLIF(@p1, ''),
		       section    = NULLIF(@p2, ''),
		       valid_from = @p3,
		       valid_to   = @p4,
		       max_uses   = @p5
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p6)`
	res, err := r.db.ExecContext(ctx, q,
		k.Grade, k.Section,
		nullableTime(k.ValidFrom), nullableTime(k.ValidTo),
		k.MaxUses, string(k.ID))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrKeyNotFound
	}
	return nil
}

func (r *KeyRepo) FindByID(ctx context.Context, id domain.KeyID) (*domain.Key, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+keyCols+` FROM [key] WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`, string(id))
	return scanKey(row)
}

func (r *KeyRepo) FindByCode(ctx context.Context, code string) (*domain.Key, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+keyCols+` FROM [key] WHERE code = @p1`, code)
	return scanKey(row)
}

func (r *KeyRepo) SetActive(ctx context.Context, id domain.KeyID, active bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE [key] SET active = @p1 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		active, string(id))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrKeyNotFound
	}
	return nil
}

func (r *KeyRepo) ListByAsesor(ctx context.Context, asesorID domain.UserID) ([]domain.Key, error) {
	return r.list(ctx,
		`SELECT `+keyCols+` FROM [key]
		  WHERE asesor_user_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		  ORDER BY created_at DESC`,
		string(asesorID))
}

func (r *KeyRepo) ListByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.Key, error) {
	return r.list(ctx,
		`SELECT `+keyCols+` FROM [key]
		  WHERE school_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		  ORDER BY created_at DESC`,
		string(schoolID))
}

func (r *KeyRepo) list(ctx context.Context, query, arg string) ([]domain.Key, error) {
	rows, err := r.db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Key
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

// IncrementUses: UPDATE atómico con guard de aforo.
//   * SET current_uses = current_uses + 1
//   * WHERE active = 1 AND (max_uses = 0 OR current_uses < max_uses)
//     AND ventana temporal vigente.
// Si afecta 0 filas, la key no era usable (carrera o inválida).
func (r *KeyRepo) IncrementUses(ctx context.Context, id domain.KeyID) (int64, error) {
	const q = `
		UPDATE [key]
		   SET current_uses = current_uses + 1
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)
		   AND active = 1
		   AND (max_uses = 0 OR current_uses < max_uses)
		   AND (valid_from IS NULL OR valid_from <= SYSUTCDATETIME())
		   AND (valid_to   IS NULL OR valid_to   >= SYSUTCDATETIME())`
	res, err := r.db.ExecContext(ctx, q, string(id))
	if err != nil {
		return 0, err
	}
	rows, _ := res.RowsAffected()
	return rows, nil
}

func mapDuplicate(err error) error {
	var msErr mssql.Error
	if errors.As(err, &msErr) && (msErr.Number == 2627 || msErr.Number == 2601) {
		if strings.Contains(strings.ToLower(msErr.Message), "uk_key_code") {
			return domain.ErrKeyCodeTaken
		}
	}
	return err
}

// nullableTime → sql.NullTime para que el driver lo serialice como NULL
// cuando t es nil.
func nullableTime(t *time.Time) any {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

// rowScanner permite reutilizar scanKey con *sql.Row y *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanKey(s rowScanner) (*domain.Key, error) {
	var (
		k         domain.Key
		idStr     string
		schoolID  string
		asesorID  string
		mode      string
		validFrom sql.NullTime
		validTo   sql.NullTime
		updatedAt sql.NullTime
	)
	err := s.Scan(
		&idStr, &k.Code, &k.ExamTypeID, &schoolID, &asesorID,
		&mode, &k.Grade, &k.Section,
		&validFrom, &validTo,
		&k.MaxUses, &k.CurrentUses, &k.Active,
		&k.CreatedAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	k.ID = domain.KeyID(idStr)
	k.SchoolID = domain.SchoolID(schoolID)
	k.AsesorUserID = domain.UserID(asesorID)
	k.Mode = domain.KeyMode(mode)
	if validFrom.Valid {
		v := validFrom.Time
		k.ValidFrom = &v
	}
	if validTo.Valid {
		v := validTo.Time
		k.ValidTo = &v
	}
	if updatedAt.Valid {
		v := updatedAt.Time
		k.UpdatedAt = &v
	}
	return &k, nil
}

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
		updated_at,
		ISNULL(CONVERT(NVARCHAR(36), exam_id), ''),
		max_attempts_per_user`

func (r *KeyRepo) Save(ctx context.Context, k *domain.Key) (domain.KeyID, error) {
	const q = `
		INSERT INTO [key] (code, exam_type_id, school_id, asesor_user_id,
		                    mode, grade, section, valid_from, valid_to,
		                    max_uses, current_uses, active, exam_id,
		                    max_attempts_per_user)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (@p1, @p2,
		        IIF(@p3 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p3)),
		        CONVERT(UNIQUEIDENTIFIER, @p4),
		        @p5, NULLIF(@p6, ''), NULLIF(@p7, ''),
		        @p8, @p9, @p10, 0, @p11,
		        IIF(@p12 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p12)),
		        @p13)`
	var id string
	err := r.db.QueryRowContext(ctx, q,
		k.Code, k.ExamTypeID, string(k.SchoolID), string(k.AsesorUserID),
		string(k.Mode), k.Grade, k.Section,
		nullableTime(k.ValidFrom), nullableTime(k.ValidTo),
		k.MaxUses, k.Active, k.ExamID, k.MaxAttemptsPerUser,
	).Scan(&id)
	if err != nil {
		return "", mapDuplicate(err)
	}
	return domain.KeyID(id), nil
}

func (r *KeyRepo) Update(ctx context.Context, k *domain.Key) error {
	const q = `
		UPDATE [key]
		   SET grade                 = NULLIF(@p1, ''),
		       section               = NULLIF(@p2, ''),
		       valid_from            = @p3,
		       valid_to              = @p4,
		       max_uses              = @p5,
		       max_attempts_per_user = @p6,
		       active                = @p7
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p8)`
	res, err := r.db.ExecContext(ctx, q,
		k.Grade, k.Section,
		nullableTime(k.ValidFrom), nullableTime(k.ValidTo),
		k.MaxUses, k.MaxAttemptsPerUser, k.Active, string(k.ID))
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

// ListAll — dump completo de la tabla. Solo lo invoca el flujo admin
// /api/admin/keys/resync-all del gateway para backfillear el sync a
// HubSpot. No usar para flujos de UI (escala mal).
func (r *KeyRepo) ListAll(ctx context.Context) ([]domain.Key, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+keyCols+` FROM [key] ORDER BY created_at ASC`)
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

// ListUserIDsByColegio devuelve los user_ids distintos que usaron keys
// de este colegio (cualquier herramienta). JOIN key_usage <-> [key]
// con filtro key.school_id. Excluye key_usage huerfanos sin user_id.
func (r *KeyRepo) ListUserIDsByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.UserID, error) {
	const q = `
		SELECT DISTINCT CONVERT(NVARCHAR(36), ku.user_id)
		  FROM key_usage ku
		  JOIN [key] k ON ku.key_id = k.id
		 WHERE k.school_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		   AND ku.user_id IS NOT NULL`
	rows, err := r.db.QueryContext(ctx, q, string(schoolID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.UserID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, domain.UserID(id))
	}
	return out, rows.Err()
}

// ListStudentKeyInfoByColegio: por cada uso de key del colegio, el grado/
// sección/exam_type de la key. Ordenado por used_at DESC para que el caller
// tome la key más reciente por user. Grado/sección viven en la key, no en
// el user — esto enriquece el listado "Ver estudiantes".
func (r *KeyRepo) ListStudentKeyInfoByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.StudentKeyInfo, error) {
	const q = `
		SELECT CONVERT(NVARCHAR(36), ku.user_id),
		       ISNULL(k.grade, ''), ISNULL(k.section, ''),
		       k.exam_type_id, ku.used_at
		  FROM key_usage ku
		  JOIN [key] k ON ku.key_id = k.id
		 WHERE k.school_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		   AND ku.user_id IS NOT NULL
		 ORDER BY ku.used_at DESC`
	rows, err := r.db.QueryContext(ctx, q, string(schoolID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.StudentKeyInfo
	for rows.Next() {
		var si domain.StudentKeyInfo
		var uid string
		if err := rows.Scan(&uid, &si.Grade, &si.Section, &si.ExamTypeID, &si.UsedAt); err != nil {
			return nil, err
		}
		si.UserID = domain.UserID(uid)
		out = append(out, si)
	}
	return out, rows.Err()
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

// IncrementUses: UPDATE atomico con guard de aforo.
//
// Semantica: max_uses cuenta USUARIOS DISTINTOS (no attempts totales). Un
// estudiante que usa la key por primera vez ocupa una plaza; sus retries
// (gobernados por max_attempts_per_user a nivel exams_service) NO consumen
// plaza adicional.
//
// Implementacion:
//   * SET current_uses = current_uses + 1
//   * WHERE active=1, ventana vigente, aforo disponible,
//     y NOT EXISTS de key_usage previo (key_id, user_id).
// El NOT EXISTS dentro del UPDATE evita race condition entre dos requests
// concurrentes del mismo usuario.
//
// Devuelve filas afectadas:
//   - 1: primer uso del usuario, plaza consumida.
//   - 0: o el usuario YA tenia key_usage (retry permitido, no consume plaza),
//        o la key no es usable. El caller debe distinguir consultando
//        KeyUsageRepository.ExistsByKeyUser.
func (r *KeyRepo) IncrementUses(ctx context.Context, id domain.KeyID, userID domain.UserID) (int64, error) {
	const q = `
		UPDATE [key]
		   SET current_uses = current_uses + 1
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)
		   AND active = 1
		   AND (max_uses = 0 OR current_uses < max_uses)
		   AND (valid_from IS NULL OR valid_from <= SYSUTCDATETIME())
		   AND (valid_to   IS NULL OR valid_to   >= SYSUTCDATETIME())
		   AND NOT EXISTS (
		         SELECT 1 FROM key_usage
		          WHERE key_id  = CONVERT(UNIQUEIDENTIFIER, @p1)
		            AND user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		       )`
	res, err := r.db.ExecContext(ctx, q, string(id), string(userID))
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
		examID    string
	)
	err := s.Scan(
		&idStr, &k.Code, &k.ExamTypeID, &schoolID, &asesorID,
		&mode, &k.Grade, &k.Section,
		&validFrom, &validTo,
		&k.MaxUses, &k.CurrentUses, &k.Active,
		&k.CreatedAt, &updatedAt,
		&examID, &k.MaxAttemptsPerUser,
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
	k.ExamID = examID
	return &k, nil
}

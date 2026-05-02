// Package mssqladapter implementa los puertos outbound de users_service
// contra Azure SQL Database (driver github.com/microsoft/go-mssqldb).
//
// Convenciones T-SQL:
//   - Parámetros nombrados @p1, @p2, ... (orden = orden de los args).
//   - UNIQUEIDENTIFIER en lugar de UUID; se serializan a string vía CAST.
//   - DATETIME2(7) en UTC (SYSUTCDATETIME() para defaults / TouchLastAccess).
//   - SetActive y similares usan @@ROWCOUNT para detectar "no encontrado".
//   - INSERT con OUTPUT INSERTED.id para recuperar la PK generada.
package mssqladapter

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	mssql "github.com/microsoft/go-mssqldb"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

// UserRepo implementa ports.UserRepository contra Azure SQL.
// Embebe *SearchEngine para ganar Search() por promoción de métodos —
// la única pieza específica de users es userSearchSchema (user_schema.go).
type UserRepo struct {
	db *sql.DB
	*SearchEngine
}

var _ ports.UserRepository = (*UserRepo)(nil)

func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{
		db:           db,
		SearchEngine: NewSearchEngine(db, userSearchSchema),
	}
}

// userCols selecciona todas las columnas en el orden que scanUser espera.
// CONVERT(...) NVARCHAR(36) para que UNIQUEIDENTIFIER llegue como string.
const userCols = `CONVERT(NVARCHAR(36), id),
		email,
		password_hash,
		ISNULL(first_name, ''),
		ISNULL(last_name, ''),
		ISNULL(document_number, ''),
		ISNULL(CONVERT(NVARCHAR(36), school_id), ''),
		active,
		last_access_at,
		created_at,
		updated_at,
		ISNULL(hubspot_record_id, '')`

func (r *UserRepo) Save(ctx context.Context, u *domain.User) (domain.UserID, error) {
	// La BD genera el UNIQUEIDENTIFIER vía DEFAULT NEWID(); lo recuperamos
	// con OUTPUT INSERTED.id (equivalente a RETURNING en Postgres).
	const q = `
		INSERT INTO users (email, password_hash, first_name, last_name, document_number, school_id, active)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (@p1, @p2, NULLIF(@p3, ''), NULLIF(@p4, ''), NULLIF(@p5, ''),
		        IIF(@p6 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p6)), @p7)`

	var id string
	err := r.db.QueryRowContext(ctx, q,
		string(u.Email), u.PasswordHash,
		u.FirstName, u.LastName, u.DocumentNumber,
		string(u.SchoolID), u.Active,
	).Scan(&id)
	if err != nil {
		return "", mapDuplicate(err)
	}
	return domain.UserID(id), nil
}

func (r *UserRepo) FindByID(ctx context.Context, id domain.UserID) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(id))
	return scanUser(row)
}

func (r *UserRepo) FindByEmail(ctx context.Context, email domain.Email) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM users WHERE email = @p1`,
		string(email))
	return scanUser(row)
}

func (r *UserRepo) ExistsByDocument(ctx context.Context, doc string) (bool, error) {
	var exists int
	err := r.db.QueryRowContext(ctx,
		`SELECT CASE WHEN EXISTS (SELECT 1 FROM users WHERE document_number = @p1) THEN 1 ELSE 0 END`,
		doc).Scan(&exists)
	return exists == 1, err
}

func (r *UserRepo) Update(ctx context.Context, u *domain.User) error {
	const q = `
		UPDATE users
		   SET first_name      = NULLIF(@p1, ''),
		       last_name       = NULLIF(@p2, ''),
		       document_number = NULLIF(@p3, ''),
		       school_id       = IIF(@p4 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p4))
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p5)`
	res, err := r.db.ExecContext(ctx, q,
		u.FirstName, u.LastName, u.DocumentNumber, string(u.SchoolID), string(u.ID))
	if err != nil {
		return mapDuplicate(err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *UserRepo) SetActive(ctx context.Context, id domain.UserID, active bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE users SET active = @p1 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		active, string(id))
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *UserRepo) TouchLastAccess(ctx context.Context, id domain.UserID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET last_access_at = SYSUTCDATETIME() WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(id))
	return err
}

// SetHubspotRecordID guarda el record_id que devuelve HubSpot al crear
// un contact. Lo invoca el hubspot-service vía gRPC tras un sync exitoso.
// Sin este id, los updates posteriores requerirían search-by-DNI/email
// (frágil, era el TODO #1 de P1).
func (r *UserRepo) SetHubspotRecordID(ctx context.Context, id domain.UserID, recordID string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE users SET hubspot_record_id = NULLIF(@p1, '')
		  WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		recordID, string(id))
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

// --------- helpers ---------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (*domain.User, error) {
	var (
		u          domain.User
		idStr      string
		emailStr   string
		firstName  string
		lastName   string
		doc        string
		schoolID   string
		active     bool
		lastAccess sql.NullTime
		updatedAt  sql.NullTime
		hubspotID  string
	)

	err := row.Scan(&idStr, &emailStr, &u.PasswordHash, &firstName, &lastName,
		&doc, &schoolID, &active, &lastAccess, &u.CreatedAt, &updatedAt, &hubspotID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	u.ID = domain.UserID(idStr)
	u.Email = domain.Email(emailStr)
	u.FirstName = firstName
	u.LastName = lastName
	u.DocumentNumber = doc
	u.SchoolID = domain.SchoolID(schoolID)
	u.Active = active
	u.HubspotRecordID = hubspotID
	if lastAccess.Valid {
		t := lastAccess.Time
		u.LastAccessAt = &t
	}
	if updatedAt.Valid {
		t := updatedAt.Time
		u.UpdatedAt = &t
	}
	return &u, nil
}

// mapDuplicate traduce errores de SQL Server a errores de dominio.
// Codes 2627 (PK/UNIQUE constraint) y 2601 (UNIQUE INDEX) cubren violaciones
// de unicidad. El adapter es quien conoce los códigos del driver; el core
// solo ve ErrEmailTaken / ErrDocumentTaken.
func mapDuplicate(err error) error {
	var msErr mssql.Error
	if errors.As(err, &msErr) && (msErr.Number == 2627 || msErr.Number == 2601) {
		msg := strings.ToLower(msErr.Message)
		switch {
		case strings.Contains(msg, "uk_user_email"), strings.Contains(msg, "users") && strings.Contains(msg, "email"):
			return domain.ErrEmailTaken
		case strings.Contains(msg, "uk_user_document_number"), strings.Contains(msg, "document"):
			return domain.ErrDocumentTaken
		}
	}
	return err
}

// _ ensures we keep the time import even if the adapter ever drops the
// struct's *time.Time fields locally (cheap defensive pin).
var _ = time.Time{}

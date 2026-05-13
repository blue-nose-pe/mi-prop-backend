package mssqladapter

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

type SchoolRepo struct {
	db *sql.DB
}

var _ ports.SchoolRepository = (*SchoolRepo)(nil)

func NewSchoolRepo(db *sql.DB) *SchoolRepo { return &SchoolRepo{db: db} }

func (r *SchoolRepo) FindByID(ctx context.Context, id domain.SchoolID) (*domain.School, error) {
	const q = `SELECT CONVERT(NVARCHAR(36), id),
	                  CONVERT(NVARCHAR(36), user_id),
	                  name, active, created_at, updated_at,
	                  ISNULL(hubspot_record_id, '')
	             FROM school WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`

	var (
		s         domain.School
		idStr     string
		userIDStr string
		updatedAt sql.NullTime
		hubspotID string
	)
	err := r.db.QueryRowContext(ctx, q, string(id)).
		Scan(&idStr, &userIDStr, &s.Name, &s.Active, &s.CreatedAt, &updatedAt, &hubspotID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrSchoolNotFound
	}
	if err != nil {
		return nil, err
	}
	s.ID = domain.SchoolID(idStr)
	s.UserID = domain.UserID(userIDStr)
	s.HubspotRecordID = hubspotID
	if updatedAt.Valid {
		t := updatedAt.Time
		s.UpdatedAt = &t
	}
	return &s, nil
}

// List devuelve schools paginados, opcionalmente filtrando por nombre
// (LIKE %search%, case-insensitive via COLLATE) y/o solo activos. Devuelve
// también el total sin paginar para que la UI pueda armar paginación.
func (r *SchoolRepo) List(ctx context.Context, in ports.ListSchoolsInput) ([]domain.School, uint32, error) {
	if in.Limit == 0 || in.Limit > 1000 {
		in.Limit = 100
	}
	where := "WHERE 1=1"
	args := []any{}
	idx := 1
	if in.ActiveOnly {
		where += " AND active = 1"
	}
	if in.Search != "" {
		where += " AND name COLLATE Latin1_General_CI_AI LIKE @p" + strconv.Itoa(idx)
		args = append(args, "%"+in.Search+"%")
		idx++
	}

	var total uint32
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM school "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	q := `SELECT CONVERT(NVARCHAR(36), id),
	             ISNULL(CONVERT(NVARCHAR(36), user_id), ''),
	             name, active, created_at, updated_at,
	             ISNULL(hubspot_record_id, '')
	        FROM school ` + where + `
	       ORDER BY name ASC
	      OFFSET @p` + strconv.Itoa(idx) + ` ROWS FETCH NEXT @p` + strconv.Itoa(idx+1) + ` ROWS ONLY`
	args = append(args, in.Offset, in.Limit)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]domain.School, 0)
	for rows.Next() {
		var (
			s         domain.School
			idStr     string
			userIDStr string
			updatedAt sql.NullTime
			hubspotID string
		)
		if err := rows.Scan(&idStr, &userIDStr, &s.Name, &s.Active, &s.CreatedAt, &updatedAt, &hubspotID); err != nil {
			return nil, 0, err
		}
		s.ID = domain.SchoolID(idStr)
		s.UserID = domain.UserID(userIDStr)
		s.HubspotRecordID = hubspotID
		if updatedAt.Valid {
			t := updatedAt.Time
			s.UpdatedAt = &t
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// Create persiste un colegio nuevo. La BD genera el UNIQUEIDENTIFIER vía
// DEFAULT NEWID() y lo recuperamos con OUTPUT INSERTED.id.
func (r *SchoolRepo) Create(ctx context.Context, s *domain.School) (domain.SchoolID, error) {
	const q = `
		INSERT INTO school (user_id, name, active, hubspot_record_id)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1), @p2, @p3, NULLIF(@p4, ''))`
	var id string
	err := r.db.QueryRowContext(ctx, q,
		string(s.UserID), s.Name, s.Active, s.HubspotRecordID,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	return domain.SchoolID(id), nil
}

// Update aplica cambios parciales (campos vacíos no se tocan).
func (r *SchoolRepo) Update(ctx context.Context, s *domain.School) error {
	const q = `
		UPDATE school
		   SET name              = COALESCE(NULLIF(@p1, ''), name),
		       user_id           = COALESCE(IIF(@p2 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p2)), user_id),
		       hubspot_record_id = COALESCE(NULLIF(@p3, ''), hubspot_record_id)
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p4)`
	res, err := r.db.ExecContext(ctx, q,
		s.Name, string(s.UserID), s.HubspotRecordID, string(s.ID))
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrSchoolNotFound
	}
	return nil
}

// SetHubspotRecordID guarda el record_id que devuelve HubSpot al crear
// un colegio (custom object o company). Lo invoca el hubspot-service vía
// gRPC tras un sync exitoso.
func (r *SchoolRepo) SetHubspotRecordID(ctx context.Context, id domain.SchoolID, recordID string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE school SET hubspot_record_id = NULLIF(@p1, '')
		  WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		recordID, string(id))
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrSchoolNotFound
	}
	return nil
}

package mssqladapter

import (
	"context"
	"database/sql"
	"errors"

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

// Package mssqladapter implementa los puertos outbound de exams_service
// contra Azure SQL Database.
package mssqladapter

import (
	"context"
	"database/sql"
	"errors"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

type ExamTypeRepo struct {
	db *sql.DB
}

var _ ports.ExamTypeRepository = (*ExamTypeRepo)(nil)

func NewExamTypeRepo(db *sql.DB) *ExamTypeRepo { return &ExamTypeRepo{db: db} }

func (r *ExamTypeRepo) FindByCode(ctx context.Context, code string) (*domain.ExamType, error) {
	const q = `SELECT id, code, name, active, created_at, updated_at
	             FROM exam_type WHERE code = @p1`
	return r.scan(r.db.QueryRowContext(ctx, q, code))
}

func (r *ExamTypeRepo) FindByID(ctx context.Context, id int32) (*domain.ExamType, error) {
	const q = `SELECT id, code, name, active, created_at, updated_at
	             FROM exam_type WHERE id = @p1`
	return r.scan(r.db.QueryRowContext(ctx, q, id))
}

func (r *ExamTypeRepo) scan(row *sql.Row) (*domain.ExamType, error) {
	var (
		t         domain.ExamType
		updatedAt sql.NullTime
	)
	err := row.Scan(&t.ID, &t.Code, &t.Name, &t.Active, &t.CreatedAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrExamTypeNotFound
	}
	if err != nil {
		return nil, err
	}
	if updatedAt.Valid {
		v := updatedAt.Time
		t.UpdatedAt = &v
	}
	return &t, nil
}

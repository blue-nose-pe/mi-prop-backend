package mssqladapter

import (
	"context"
	"database/sql"
	"errors"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

type QuestionRepo struct {
	db *sql.DB
	*SearchEngine
}

var _ ports.QuestionRepository = (*QuestionRepo)(nil)

func NewQuestionRepo(db *sql.DB) *QuestionRepo {
	return &QuestionRepo{db: db, SearchEngine: NewSearchEngine(db, questionSearchSchema)}
}

const questionCols = `CONVERT(NVARCHAR(36), id),
		[text],
		ISNULL(category, ''),
		active,
		created_at,
		updated_at`

func (r *QuestionRepo) Save(ctx context.Context, q *domain.Question) (domain.QuestionID, error) {
	const sqlText = `
		INSERT INTO question ([text], category, active)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (@p1, NULLIF(@p2, ''), @p3)`
	var id string
	if err := r.db.QueryRowContext(ctx, sqlText, q.Text, q.Category, q.Active).Scan(&id); err != nil {
		return "", err
	}
	return domain.QuestionID(id), nil
}

func (r *QuestionRepo) Update(ctx context.Context, q *domain.Question) error {
	const sqlText = `
		UPDATE question
		   SET [text] = @p1, category = NULLIF(@p2, '')
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p3)`
	res, err := r.db.ExecContext(ctx, sqlText, q.Text, q.Category, string(q.ID))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrQuestionNotFound
	}
	return nil
}

func (r *QuestionRepo) FindByID(ctx context.Context, id domain.QuestionID) (*domain.Question, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+questionCols+` FROM question WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(id))

	var (
		q         domain.Question
		idStr     string
		updatedAt sql.NullTime
	)
	err := row.Scan(&idStr, &q.Text, &q.Category, &q.Active, &q.CreatedAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrQuestionNotFound
	}
	if err != nil {
		return nil, err
	}
	q.ID = domain.QuestionID(idStr)
	if updatedAt.Valid {
		v := updatedAt.Time
		q.UpdatedAt = &v
	}
	return &q, nil
}

func (r *QuestionRepo) SetActive(ctx context.Context, id domain.QuestionID, active bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE question SET active = @p1 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		active, string(id))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrQuestionNotFound
	}
	return nil
}

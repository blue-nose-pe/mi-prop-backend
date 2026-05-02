package mssqladapter

import (
	"context"
	"database/sql"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

type OptionRepo struct {
	db *sql.DB
}

var _ ports.QuestionOptionRepository = (*OptionRepo)(nil)

func NewOptionRepo(db *sql.DB) *OptionRepo { return &OptionRepo{db: db} }

func (r *OptionRepo) Save(ctx context.Context, o *domain.QuestionOption) (domain.OptionID, error) {
	const q = `
		INSERT INTO question_option (question_id, [text], is_correct, sort_order)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1), @p2, @p3, @p4)`
	var id string
	if err := r.db.QueryRowContext(ctx, q,
		string(o.QuestionID), o.Text, o.IsCorrect, o.SortOrder,
	).Scan(&id); err != nil {
		return "", err
	}
	return domain.OptionID(id), nil
}

func (r *OptionRepo) Update(ctx context.Context, o *domain.QuestionOption) error {
	const q = `
		UPDATE question_option
		   SET [text] = @p1, is_correct = @p2, sort_order = @p3
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p4)`
	res, err := r.db.ExecContext(ctx, q, o.Text, o.IsCorrect, o.SortOrder, string(o.ID))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrOptionNotFound
	}
	return nil
}

func (r *OptionRepo) Delete(ctx context.Context, id domain.OptionID) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM question_option WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(id))
	return err
}

func (r *OptionRepo) ListByQuestion(ctx context.Context, qID domain.QuestionID) ([]domain.QuestionOption, error) {
	const q = `
		SELECT CONVERT(NVARCHAR(36), id),
		       CONVERT(NVARCHAR(36), question_id),
		       [text], is_correct, sort_order
		  FROM question_option
		 WHERE question_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		 ORDER BY sort_order ASC, id ASC`
	rows, err := r.db.QueryContext(ctx, q, string(qID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.QuestionOption
	for rows.Next() {
		var (
			o         domain.QuestionOption
			idStr     string
			questionID string
		)
		if err := rows.Scan(&idStr, &questionID, &o.Text, &o.IsCorrect, &o.SortOrder); err != nil {
			return nil, err
		}
		o.ID = domain.OptionID(idStr)
		o.QuestionID = domain.QuestionID(questionID)
		out = append(out, o)
	}
	return out, rows.Err()
}

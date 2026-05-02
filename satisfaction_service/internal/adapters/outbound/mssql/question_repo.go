package mssqladapter

import (
	"context"
	"database/sql"
	"errors"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
)

type QuestionRepo struct {
	db *sql.DB
}

var _ ports.QuestionRepository = (*QuestionRepo)(nil)

func NewQuestionRepo(db *sql.DB) *QuestionRepo { return &QuestionRepo{db: db} }

const questionCols = `CONVERT(NVARCHAR(36), id),
		CONVERT(NVARCHAR(36), survey_id),
		[text],
		kind,
		sort_order,
		ISNULL(options_json, ''),
		required`

func (r *QuestionRepo) Save(ctx context.Context, q *domain.Question) (domain.QuestionID, error) {
	const sqlText = `
		INSERT INTO survey_question (survey_id, [text], kind, sort_order, options_json, required)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1), @p2, @p3, @p4, NULLIF(@p5, ''), @p6)`
	var id string
	err := r.db.QueryRowContext(ctx, sqlText,
		string(q.SurveyID), q.Text, string(q.Kind), q.SortOrder, q.OptionsJSON, q.Required,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	return domain.QuestionID(id), nil
}

func (r *QuestionRepo) Update(ctx context.Context, q *domain.Question) error {
	const sqlText = `
		UPDATE survey_question
		   SET [text] = @p1, sort_order = @p2, options_json = NULLIF(@p3, ''), required = @p4
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p5)`
	res, err := r.db.ExecContext(ctx, sqlText, q.Text, q.SortOrder, q.OptionsJSON, q.Required, string(q.ID))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrQuestionNotFound
	}
	return nil
}

func (r *QuestionRepo) Delete(ctx context.Context, id domain.QuestionID) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM survey_question WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(id))
	return err
}

func (r *QuestionRepo) FindByID(ctx context.Context, id domain.QuestionID) (*domain.Question, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+questionCols+` FROM survey_question WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(id))
	q, err := scanQuestion(row)
	return q, err
}

func (r *QuestionRepo) ListBySurvey(ctx context.Context, surveyID domain.SurveyID) ([]domain.Question, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+questionCols+` FROM survey_question
		  WHERE survey_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		  ORDER BY sort_order ASC, id ASC`,
		string(surveyID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Question
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *q)
	}
	return out, rows.Err()
}

func scanQuestion(s rowScanner) (*domain.Question, error) {
	var (
		q         domain.Question
		idStr     string
		surveyID  string
		kindStr   string
	)
	err := s.Scan(&idStr, &surveyID, &q.Text, &kindStr, &q.SortOrder, &q.OptionsJSON, &q.Required)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrQuestionNotFound
	}
	if err != nil {
		return nil, err
	}
	q.ID = domain.QuestionID(idStr)
	q.SurveyID = domain.SurveyID(surveyID)
	q.Kind = domain.QuestionKind(kindStr)
	return &q, nil
}

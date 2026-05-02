package mssqladapter

import (
	"context"
	"database/sql"
	"errors"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
)

type ResponseRepo struct {
	db *sql.DB
}

var _ ports.ResponseRepository = (*ResponseRepo)(nil)

func NewResponseRepo(db *sql.DB) *ResponseRepo { return &ResponseRepo{db: db} }

// Submit guarda response + answers en UNA SOLA transacción.
func (r *ResponseRepo) Submit(ctx context.Context, resp *domain.Response, answers []domain.Answer) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	const insertResp = `
		INSERT INTO survey_response (survey_id, user_id, exam_attempt_id)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1),
		        CONVERT(UNIQUEIDENTIFIER, @p2),
		        IIF(@p3 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p3)))`
	var idStr string
	if err := tx.QueryRowContext(ctx, insertResp,
		string(resp.SurveyID), string(resp.UserID), string(resp.ExamAttemptID),
	).Scan(&idStr); err != nil {
		return err
	}
	resp.ID = domain.ResponseID(idStr)

	const insertAns = `
		INSERT INTO survey_answer (response_id, question_id, value_text, value_number)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1),
		        CONVERT(UNIQUEIDENTIFIER, @p2),
		        NULLIF(@p3, ''), @p4)`
	for _, a := range answers {
		var vNum any
		if a.ValueNumber != nil {
			vNum = *a.ValueNumber
		}
		if _, err := tx.ExecContext(ctx, insertAns,
			idStr, string(a.QuestionID), a.ValueText, vNum,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *ResponseRepo) FindByID(ctx context.Context, id domain.ResponseID) (*domain.Response, error) {
	const q = `
		SELECT CONVERT(NVARCHAR(36), id),
		       CONVERT(NVARCHAR(36), survey_id),
		       CONVERT(NVARCHAR(36), user_id),
		       ISNULL(CONVERT(NVARCHAR(36), exam_attempt_id), ''),
		       submitted_at
		  FROM survey_response WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`
	var (
		resp     domain.Response
		idStr    string
		surveyID string
		userID   string
		attempt  string
	)
	err := r.db.QueryRowContext(ctx, q, string(id)).Scan(
		&idStr, &surveyID, &userID, &attempt, &resp.SubmittedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrResponseNotFound
	}
	if err != nil {
		return nil, err
	}
	resp.ID = domain.ResponseID(idStr)
	resp.SurveyID = domain.SurveyID(surveyID)
	resp.UserID = domain.UserID(userID)
	resp.ExamAttemptID = domain.AttemptID(attempt)
	return &resp, nil
}

// GetMetricsRaw: cosecha las respuestas, las preguntas y total para que
// el query handler haga los cálculos. Más caro que un agregado SQL puro,
// pero la lógica de NPS / averages vive en el core (testeable).
func (r *ResponseRepo) GetMetricsRaw(ctx context.Context, surveyID domain.SurveyID) (*ports.RawMetrics, error) {
	out := &ports.RawMetrics{}

	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM survey_response WHERE survey_id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(surveyID),
	).Scan(&out.TotalResponses); err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT CONVERT(NVARCHAR(36), a.response_id),
		       CONVERT(NVARCHAR(36), a.question_id),
		       ISNULL(a.value_text, ''),
		       a.value_number
		  FROM survey_answer a
		  JOIN survey_response r ON r.id = a.response_id
		 WHERE r.survey_id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(surveyID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			a       domain.Answer
			respStr string
			qIDStr  string
			vNum    sql.NullInt32
		)
		if err := rows.Scan(&respStr, &qIDStr, &a.ValueText, &vNum); err != nil {
			return nil, err
		}
		a.ResponseID = domain.ResponseID(respStr)
		a.QuestionID = domain.QuestionID(qIDStr)
		if vNum.Valid {
			v := vNum.Int32
			a.ValueNumber = &v
		}
		out.Answers = append(out.Answers, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	qrows, err := r.db.QueryContext(ctx, `
		SELECT `+questionCols+`
		  FROM survey_question
		 WHERE survey_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		 ORDER BY sort_order`,
		string(surveyID))
	if err != nil {
		return nil, err
	}
	defer qrows.Close()
	for qrows.Next() {
		q, err := scanQuestion(qrows)
		if err != nil {
			return nil, err
		}
		out.Questions = append(out.Questions, *q)
	}
	return out, qrows.Err()
}

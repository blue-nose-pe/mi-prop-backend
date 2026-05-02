package mssqladapter

import (
	"context"
	"database/sql"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

type ExamQuestionRepo struct {
	db *sql.DB
}

var _ ports.ExamQuestionRepository = (*ExamQuestionRepo)(nil)

func NewExamQuestionRepo(db *sql.DB) *ExamQuestionRepo { return &ExamQuestionRepo{db: db} }

func (r *ExamQuestionRepo) Add(ctx context.Context, eq *domain.ExamQuestion) error {
	const q = `
		IF NOT EXISTS (
		    SELECT 1 FROM exam_question
		     WHERE exam_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		       AND question_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		)
		INSERT INTO exam_question (exam_id, question_id, points, sort_order)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1),
		        CONVERT(UNIQUEIDENTIFIER, @p2),
		        @p3, @p4);`
	_, err := r.db.ExecContext(ctx, q, string(eq.ExamID), string(eq.QuestionID), eq.Points, eq.SortOrder)
	return err
}

func (r *ExamQuestionRepo) Remove(ctx context.Context, examID domain.ExamID, questionID domain.QuestionID) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM exam_question
		  WHERE exam_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		    AND question_id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		string(examID), string(questionID))
	return err
}

// Reorder actualiza el sort_order de cada pregunta del exam según el
// orden recibido. Operación en una transacción.
func (r *ExamQuestionRepo) Reorder(ctx context.Context, examID domain.ExamID, ordered []domain.QuestionID) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	const q = `
		UPDATE exam_question
		   SET sort_order = @p1
		 WHERE exam_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		   AND question_id = CONVERT(UNIQUEIDENTIFIER, @p3)`
	for i, qID := range ordered {
		if _, err := tx.ExecContext(ctx, q, int32(i), string(examID), string(qID)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *ExamQuestionRepo) List(ctx context.Context, examID domain.ExamID) ([]domain.ExamQuestion, error) {
	const q = `
		SELECT CONVERT(NVARCHAR(36), exam_id),
		       CONVERT(NVARCHAR(36), question_id),
		       points, sort_order
		  FROM exam_question
		 WHERE exam_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		 ORDER BY sort_order ASC`
	rows, err := r.db.QueryContext(ctx, q, string(examID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ExamQuestion
	for rows.Next() {
		var (
			eq         domain.ExamQuestion
			examIDStr  string
			questionID string
		)
		if err := rows.Scan(&examIDStr, &questionID, &eq.Points, &eq.SortOrder); err != nil {
			return nil, err
		}
		eq.ExamID = domain.ExamID(examIDStr)
		eq.QuestionID = domain.QuestionID(questionID)
		out = append(out, eq)
	}
	return out, rows.Err()
}

// CloneInto copia todas las exam_question de src a dst (mismo exam_type
// distintos exámenes; usado al clonar para versionar).
func (r *ExamQuestionRepo) CloneInto(ctx context.Context, src, dst domain.ExamID) error {
	const q = `
		INSERT INTO exam_question (exam_id, question_id, points, sort_order)
		SELECT CONVERT(UNIQUEIDENTIFIER, @p2), question_id, points, sort_order
		  FROM exam_question
		 WHERE exam_id = CONVERT(UNIQUEIDENTIFIER, @p1)`
	_, err := r.db.ExecContext(ctx, q, string(src), string(dst))
	return err
}

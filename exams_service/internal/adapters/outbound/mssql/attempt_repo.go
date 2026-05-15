package mssqladapter

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

type AttemptRepo struct {
	db *sql.DB
}

var _ ports.AttemptRepository = (*AttemptRepo)(nil)

func NewAttemptRepo(db *sql.DB) *AttemptRepo { return &AttemptRepo{db: db} }

const attemptCols = `CONVERT(NVARCHAR(36), id),
		CONVERT(NVARCHAR(36), exam_id),
		CONVERT(NVARCHAR(36), user_id),
		ISNULL(CONVERT(NVARCHAR(36), key_id), ''),
		score,
		max_score,
		started_at,
		submitted_at`

func (r *AttemptRepo) Save(ctx context.Context, a *domain.ExamAttempt) (domain.AttemptID, error) {
	const q = `
		INSERT INTO exam_attempt (exam_id, user_id, key_id)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1),
		        CONVERT(UNIQUEIDENTIFIER, @p2),
		        IIF(@p3 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p3)))`
	var id string
	if err := r.db.QueryRowContext(ctx, q, string(a.ExamID), string(a.UserID), string(a.KeyID)).Scan(&id); err != nil {
		return "", err
	}
	return domain.AttemptID(id), nil
}

func (r *AttemptRepo) FindByID(ctx context.Context, id domain.AttemptID) (*domain.ExamAttempt, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+attemptCols+` FROM exam_attempt WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(id))
	return scanAttempt(row)
}

func (r *AttemptRepo) ListByUser(ctx context.Context, userID domain.UserID) ([]domain.ExamAttempt, error) {
	return r.list(ctx,
		`SELECT `+attemptCols+` FROM exam_attempt
		  WHERE user_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		  ORDER BY started_at DESC`,
		string(userID))
}

func (r *AttemptRepo) ListByExam(ctx context.Context, examID domain.ExamID) ([]domain.ExamAttempt, error) {
	return r.list(ctx,
		`SELECT `+attemptCols+` FROM exam_attempt
		  WHERE exam_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		  ORDER BY started_at DESC`,
		string(examID))
}

// ListByColegio: attempts cuyos users pertenecen al colegio dado. Hace
// cross-DB JOIN a db_users.dbo.users; ambas DBs viven en la misma
// instancia (mssql-server) y db_users esta accesible con permisos del
// mismo user SQL.
func (r *AttemptRepo) ListByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.ExamAttempt, error) {
	return r.list(ctx,
		`SELECT `+attemptCols+`
		   FROM exam_attempt a
		  WHERE a.user_id IN (
		      SELECT id FROM db_users.dbo.users
		       WHERE school_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		  )
		  ORDER BY a.started_at DESC`,
		string(schoolID))
}

func (r *AttemptRepo) list(ctx context.Context, query, arg string) ([]domain.ExamAttempt, error) {
	rows, err := r.db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ExamAttempt
	for rows.Next() {
		a, err := scanAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// UpsertAnswer: idempotente — si ya existía respuesta para esa pregunta,
// se sobreescribe el option_id (un attempt elige UNA opción por pregunta).
func (r *AttemptRepo) UpsertAnswer(ctx context.Context, ans *domain.AttemptAnswer) error {
	const q = `
		MERGE INTO attempt_answer AS target
		USING (SELECT
		           CONVERT(UNIQUEIDENTIFIER, @p1) AS exam_attempt_id,
		           CONVERT(UNIQUEIDENTIFIER, @p2) AS question_id,
		           CONVERT(UNIQUEIDENTIFIER, @p3) AS question_option_id
		      ) AS source
		ON target.exam_attempt_id = source.exam_attempt_id
		   AND target.question_id = source.question_id
		WHEN MATCHED THEN
		    UPDATE SET question_option_id = source.question_option_id,
		               answered_at = SYSUTCDATETIME()
		WHEN NOT MATCHED THEN
		    INSERT (exam_attempt_id, question_id, question_option_id)
		    VALUES (source.exam_attempt_id, source.question_id, source.question_option_id);`
	_, err := r.db.ExecContext(ctx, q, string(ans.AttemptID), string(ans.QuestionID), string(ans.OptionID))
	return err
}

func (r *AttemptRepo) ListAnswers(ctx context.Context, attemptID domain.AttemptID) ([]domain.AttemptAnswer, error) {
	const q = `
		SELECT CONVERT(NVARCHAR(36), exam_attempt_id),
		       CONVERT(NVARCHAR(36), question_id),
		       CONVERT(NVARCHAR(36), question_option_id),
		       answered_at
		  FROM attempt_answer
		 WHERE exam_attempt_id = CONVERT(UNIQUEIDENTIFIER, @p1)`
	rows, err := r.db.QueryContext(ctx, q, string(attemptID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.AttemptAnswer
	for rows.Next() {
		var (
			a          domain.AttemptAnswer
			attemptStr string
			questionID string
			optionID   string
		)
		if err := rows.Scan(&attemptStr, &questionID, &optionID, &a.AnsweredAt); err != nil {
			return nil, err
		}
		a.AttemptID = domain.AttemptID(attemptStr)
		a.QuestionID = domain.QuestionID(questionID)
		a.OptionID = domain.OptionID(optionID)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *AttemptRepo) ListEnrichedAnswers(ctx context.Context, attemptID domain.AttemptID) ([]domain.EnrichedAnswer, error) {
	const q = `
		SELECT CONVERT(NVARCHAR(36), aa.question_id),
		       q.text,
		       ISNULL(q.category, ''),
		       CONVERT(NVARCHAR(36), aa.question_option_id),
		       qo.text,
		       qo.sort_order,
		       qo.is_correct,
		       aa.answered_at
		  FROM attempt_answer aa
		  JOIN question        q  ON q.id  = aa.question_id
		  JOIN question_option qo ON qo.id = aa.question_option_id
		 WHERE aa.exam_attempt_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		 ORDER BY q.text`
	rows, err := r.db.QueryContext(ctx, q, string(attemptID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.EnrichedAnswer
	for rows.Next() {
		var (
			a          domain.EnrichedAnswer
			questionID string
			optionID   string
		)
		if err := rows.Scan(
			&questionID, &a.QuestionText, &a.QuestionCategory,
			&optionID, &a.OptionText, &a.OptionSortOrder, &a.OptionIsCorrect,
			&a.AnsweredAt,
		); err != nil {
			return nil, err
		}
		a.QuestionID = domain.QuestionID(questionID)
		a.OptionID = domain.OptionID(optionID)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *AttemptRepo) Finish(ctx context.Context, id domain.AttemptID, score, maxScore int32, when time.Time) error {
	const q = `
		UPDATE exam_attempt
		   SET score = @p1, max_score = @p2, submitted_at = @p3
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p4)
		   AND submitted_at IS NULL`
	res, err := r.db.ExecContext(ctx, q, score, maxScore, when, string(id))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrAttemptNotFound
	}
	return nil
}

// CountActiveByExam: cuenta attempts no descartados (una sola fila por
// user/exam mientras submitted_at sea NULL o reciente).
func (r *AttemptRepo) CountActiveByExam(ctx context.Context, examID domain.ExamID) (int32, error) {
	var count int32
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM exam_attempt WHERE exam_id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(examID)).Scan(&count)
	return count, err
}

func scanAttempt(s rowScanner) (*domain.ExamAttempt, error) {
	var (
		a          domain.ExamAttempt
		idStr      string
		examID     string
		userID     string
		keyID      string
		score      sql.NullInt32
		maxScore   sql.NullInt32
		submitted  sql.NullTime
	)
	err := s.Scan(&idStr, &examID, &userID, &keyID, &score, &maxScore, &a.StartedAt, &submitted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrAttemptNotFound
	}
	if err != nil {
		return nil, err
	}
	a.ID = domain.AttemptID(idStr)
	a.ExamID = domain.ExamID(examID)
	a.UserID = domain.UserID(userID)
	a.KeyID = domain.KeyID(keyID)
	if score.Valid {
		v := score.Int32
		a.Score = &v
	}
	if maxScore.Valid {
		v := maxScore.Int32
		a.MaxScore = &v
	}
	if submitted.Valid {
		t := submitted.Time
		a.SubmittedAt = &t
	}
	return &a, nil
}

// rowScanner permite reutilizar scanAttempt para *sql.Row y *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

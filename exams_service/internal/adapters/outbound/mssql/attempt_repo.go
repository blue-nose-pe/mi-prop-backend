package mssqladapter

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"

	mssql "github.com/microsoft/go-mssqldb"
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
		submitted_at,
		ISNULL(duration_minutes, 0)`

func (r *AttemptRepo) Save(ctx context.Context, a *domain.ExamAttempt) (domain.AttemptID, error) {
	const q = `
		INSERT INTO exam_attempt (exam_id, user_id, key_id, duration_minutes)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1),
		        CONVERT(UNIQUEIDENTIFIER, @p2),
		        IIF(@p3 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p3)),
		        @p4)`
	var id string
	if err := r.db.QueryRowContext(ctx, q, string(a.ExamID), string(a.UserID), string(a.KeyID), a.DurationMinutes).Scan(&id); err != nil {
		return "", mapAttemptInsertError(err)
	}
	return domain.AttemptID(id), nil
}

// mapAttemptInsertError detecta violacion del filtered UNIQUE
// uq_exam_attempt_active (migration 011) — significa que otro request
// concurrente del mismo alumno ya creo el attempt activo. El core debe
// hacer retry-find y devolver Reused en vez de propagar este error.
func mapAttemptInsertError(err error) error {
	var msErr mssql.Error
	if errors.As(err, &msErr) && (msErr.Number == 2627 || msErr.Number == 2601) {
		if strings.Contains(strings.ToLower(msErr.Message), "uq_exam_attempt_active") {
			return ports.ErrConcurrentActiveAttempt
		}
	}
	return err
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

// ListByKey: attempts iniciados con la key especifica. Excluye attempts
// modo admin (key_id NULL).
func (r *AttemptRepo) ListByKey(ctx context.Context, keyID domain.KeyID) ([]domain.ExamAttempt, error) {
	return r.list(ctx,
		`SELECT `+attemptCols+` FROM exam_attempt
		  WHERE key_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		  ORDER BY started_at DESC`,
		string(keyID))
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

// UpsertAnswer (legacy SINGLE_CHOICE/TF/SCALE): idempotente — si ya
// existía respuesta para esa pregunta, se sobreescribe la fila previa.
// Implementado como DELETE+INSERT para que funcione con el PK nuevo
// (attempt, question, option_id_pk) que admite multi-fila pero aqui se
// usa con 1 sola opcion.
func (r *AttemptRepo) UpsertAnswer(ctx context.Context, ans *domain.AttemptAnswer) error {
	return r.ReplaceAnswer(ctx, ans.AttemptID, ans.QuestionID, []domain.OptionID{ans.OptionID}, "")
}

// ReplaceAnswer borra todas las filas previas del (attempt, question) y
// re-inserta:
//   - SINGLE_CHOICE / TRUE_FALSE / SCALE: 1 fila con question_option_id
//   - MULTIPLE_CHOICE: N filas, una por option_id en optionIDs
//   - OPEN_TEXT: 1 fila con question_option_id=NULL y answer_text seteado
//     (optionIDs queda vacio)
//
// Es idempotente — el alumno puede cambiar de respuesta tantas veces como
// quiera dentro del attempt y el back siempre refleja la ultima eleccion.
func (r *AttemptRepo) ReplaceAnswer(
	ctx context.Context,
	attemptID domain.AttemptID,
	questionID domain.QuestionID,
	optionIDs []domain.OptionID,
	answerText string,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM attempt_answer
		  WHERE exam_attempt_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		    AND question_id     = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		string(attemptID), string(questionID),
	); err != nil {
		return err
	}

	if len(optionIDs) == 0 {
		// OPEN_TEXT: 1 fila con option NULL y answer_text seteado. Si el
		// caller no manda texto tampoco, no insertamos nada (deja la
		// pregunta sin responder).
		if answerText == "" {
			return tx.Commit()
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO attempt_answer (exam_attempt_id, question_id, question_option_id, answer_text)
			 VALUES (CONVERT(UNIQUEIDENTIFIER, @p1), CONVERT(UNIQUEIDENTIFIER, @p2), NULL, @p3)`,
			string(attemptID), string(questionID), answerText,
		); err != nil {
			return err
		}
		return tx.Commit()
	}

	// SINGLE/TF/SCALE (1 option) o MULTIPLE_CHOICE (N options).
	for _, optID := range optionIDs {
		if optID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO attempt_answer (exam_attempt_id, question_id, question_option_id)
			 VALUES (CONVERT(UNIQUEIDENTIFIER, @p1), CONVERT(UNIQUEIDENTIFIER, @p2), CONVERT(UNIQUEIDENTIFIER, @p3))`,
			string(attemptID), string(questionID), string(optID),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
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
		       q.kind,
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
			kindStr    string
			optionID   string
		)
		if err := rows.Scan(
			&questionID, &a.QuestionText, &a.QuestionCategory, &kindStr,
			&optionID, &a.OptionText, &a.OptionSortOrder, &a.OptionIsCorrect,
			&a.AnsweredAt,
		); err != nil {
			return nil, err
		}
		a.QuestionID = domain.QuestionID(questionID)
		a.QuestionKind = domain.QuestionKind(kindStr)
		a.OptionID = domain.OptionID(optionID)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *AttemptRepo) Finish(ctx context.Context, id domain.AttemptID, score, maxScore float64, when time.Time) error {
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

// CountActiveByExam: cuenta PARTICIPANTES ÚNICOS del exam (aforo a nivel exam,
// solo path SIN key). Fix audit 2026-07-02: antes era COUNT(*) de TODAS las
// filas, así que (a) cada reintento del mismo alumno inflaba el conteo y (b) los
// attempts ya enviados/abandonados seguían ocupando plaza para siempre → el
// aforo "se llenaba" aunque nadie estuviera rindiendo. COUNT(DISTINCT user_id)
// refleja el aforo real de participantes (un alumno cuenta una sola vez).
func (r *AttemptRepo) CountActiveByExam(ctx context.Context, examID domain.ExamID) (int32, error) {
	var count int32
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT user_id) FROM exam_attempt WHERE exam_id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(examID)).Scan(&count)
	return count, err
}

// FindActiveByExamUser devuelve el attempt mas reciente sin submit del
// user para ese exam. Si no hay → (nil, nil). Si la BD retorna error
// distinto a sql.ErrNoRows → propaga.
func (r *AttemptRepo) FindActiveByExamUser(ctx context.Context, examID domain.ExamID, userID domain.UserID) (*domain.ExamAttempt, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT TOP 1 `+attemptCols+` FROM exam_attempt
		  WHERE exam_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		    AND user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		    AND submitted_at IS NULL
		  ORDER BY started_at DESC`,
		string(examID), string(userID))
	att, err := scanAttempt(row)
	if errors.Is(err, domain.ErrAttemptNotFound) {
		return nil, nil
	}
	return att, err
}

// CountSubmittedByKeyUser cuenta attempts SUBMITTED del user PARA ESA
// KEY especifica. Lo usa StartAttempt para comparar contra
// key.MaxAttemptsPerUser. El limite es POR (key, user) — no global del
// exam — para que un alumno que ya rindio con una key vieja no quede
// bloqueado al recibir una key nueva del asesor para el mismo exam.
// Solo cuenta los que llegaron a Finish (submitted_at != null).
func (r *AttemptRepo) CountSubmittedByKeyUser(ctx context.Context, keyID domain.KeyID, userID domain.UserID) (int32, error) {
	var count int32
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM exam_attempt
		  WHERE key_id  = CONVERT(UNIQUEIDENTIFIER, @p1)
		    AND user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		    AND submitted_at IS NOT NULL`,
		string(keyID), string(userID)).Scan(&count)
	return count, err
}

// FindMostRecentSubmittedByKeyUser devuelve el attempt SUBMITTED mas
// reciente de un (key, user). Lo usa el RPC publico
// AttemptService.CountSubmittedByKeyUser para que el gateway pueda
// redirigir al alumno a /resultados con el attempt previo cuando ya
// consumio sus intentos contra esa key.
func (r *AttemptRepo) FindMostRecentSubmittedByKeyUser(ctx context.Context, keyID domain.KeyID, userID domain.UserID) (*domain.ExamAttempt, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT TOP 1 `+attemptCols+` FROM exam_attempt
		  WHERE key_id  = CONVERT(UNIQUEIDENTIFIER, @p1)
		    AND user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		    AND submitted_at IS NOT NULL
		  ORDER BY submitted_at DESC`,
		string(keyID), string(userID))
	att, err := scanAttempt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return att, err
}

// FindMostRecentSubmittedByExamUser devuelve el attempt SUBMITTED mas
// reciente. Lo usa StartAttempt para devolver al alumno su ultimo
// resultado cuando ya consumio sus intentos (el front lo redirige a
// /resultados con ese attempt_id en vez de mostrarle un error pelado).
func (r *AttemptRepo) FindMostRecentSubmittedByExamUser(ctx context.Context, examID domain.ExamID, userID domain.UserID) (*domain.ExamAttempt, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT TOP 1 `+attemptCols+` FROM exam_attempt
		  WHERE exam_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		    AND user_id = CONVERT(UNIQUEIDENTIFIER, @p2)
		    AND submitted_at IS NOT NULL
		  ORDER BY submitted_at DESC`,
		string(examID), string(userID))
	att, err := scanAttempt(row)
	if errors.Is(err, domain.ErrAttemptNotFound) {
		return nil, nil
	}
	return att, err
}

func scanAttempt(s rowScanner) (*domain.ExamAttempt, error) {
	var (
		a          domain.ExamAttempt
		idStr      string
		examID     string
		userID     string
		keyID      string
		score      sql.NullFloat64
		maxScore   sql.NullFloat64
		submitted  sql.NullTime
	)
	err := s.Scan(&idStr, &examID, &userID, &keyID, &score, &maxScore, &a.StartedAt, &submitted, &a.DurationMinutes)
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
		v := score.Float64
		a.Score = &v
	}
	if maxScore.Valid {
		v := maxScore.Float64
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

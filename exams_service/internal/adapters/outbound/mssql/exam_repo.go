package mssqladapter

import (
	"context"
	"database/sql"
	"errors"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

type ExamRepo struct {
	db *sql.DB
	*SearchEngine
}

var _ ports.ExamRepository = (*ExamRepo)(nil)

func NewExamRepo(db *sql.DB) *ExamRepo {
	return &ExamRepo{db: db, SearchEngine: NewSearchEngine(db, examSearchSchema)}
}

const examCols = `CONVERT(NVARCHAR(36), id),
		exam_type_id,
		ISNULL(CONVERT(NVARCHAR(36), school_id), ''),
		ISNULL(CONVERT(NVARCHAR(36), parent_exam_id), ''),
		version,
		code,
		name,
		start_at,
		end_at,
		max_participants,
		published,
		active,
		created_at,
		updated_at,
			default_points,
			default_points_incorrect,
			default_points_blank`

func (r *ExamRepo) Save(ctx context.Context, e *domain.Exam) (domain.ExamID, error) {
	const q = `
		INSERT INTO exam (exam_type_id, school_id, parent_exam_id, version, code,
		                  name, start_at, end_at, max_participants, published, active,
		                  default_points, default_points_incorrect, default_points_blank)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (@p1,
		        IIF(@p2 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p2)),
		        IIF(@p3 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p3)),
		        @p4, @p5, @p6, @p7, @p8, @p9, @p10, @p11, @p12, @p13, @p14)`
	var id string
	err := r.db.QueryRowContext(ctx, q,
		e.ExamTypeID,
		string(e.SchoolID),
		string(e.ParentExamID),
		e.Version,
		e.Code,
		e.Name,
		e.StartAt,
		e.EndAt,
		e.MaxParticipants,
		e.Published,
		e.Active,
		e.DefaultPoints,
		e.DefaultPointsIncorrect,
		e.DefaultPointsBlank,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	return domain.ExamID(id), nil
}

func (r *ExamRepo) Update(ctx context.Context, e *domain.Exam) error {
	const q = `
		UPDATE exam
		   SET name = @p1, start_at = @p2, end_at = @p3,
		       max_participants = @p4, code = @p5,
		       default_points = @p6, default_points_incorrect = @p7, default_points_blank = @p8
WHERE id = CONVERT(UNIQUEIDENTIFIER, @p9)`
	res, err := r.db.ExecContext(ctx, q, e.Name, e.StartAt, e.EndAt, e.MaxParticipants, e.Code,
		e.DefaultPoints, e.DefaultPointsIncorrect, e.DefaultPointsBlank, string(e.ID))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrExamNotFound
	}
	return nil
}

func (r *ExamRepo) FindByID(ctx context.Context, id domain.ExamID) (*domain.Exam, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+examCols+` FROM exam WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(id))

	var (
		e         domain.Exam
		idStr     string
		schoolID  string
		parentID  string
		updatedAt sql.NullTime
	)
	err := row.Scan(
		&idStr, &e.ExamTypeID, &schoolID, &parentID, &e.Version, &e.Code,
		&e.Name, &e.StartAt, &e.EndAt, &e.MaxParticipants,
		&e.Published, &e.Active, &e.CreatedAt, &updatedAt,
		&e.DefaultPoints, &e.DefaultPointsIncorrect, &e.DefaultPointsBlank,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrExamNotFound
	}
	if err != nil {
		return nil, err
	}
	e.ID = domain.ExamID(idStr)
	e.SchoolID = domain.SchoolID(schoolID)
	e.ParentExamID = domain.ExamID(parentID)
	if updatedAt.Valid {
		v := updatedAt.Time
		e.UpdatedAt = &v
	}
	return &e, nil
}

func (r *ExamRepo) SetActive(ctx context.Context, id domain.ExamID, active bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE exam SET active = @p1 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		active, string(id))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrExamNotFound
	}
	return nil
}

// MaxVersionInFamily encuentra la version mas alta entre la raiz de la familia
// (el ancestro sin parent_exam_id) y todos sus descendientes. Se usa al clonar
// para garantizar que cada clon obtenga una version unica dentro de la familia,
// incluso si el mismo exam fue clonado mas de una vez.
func (r *ExamRepo) MaxVersionInFamily(ctx context.Context, id domain.ExamID) (int32, error) {
	const q = `
		WITH ancestors AS (
		    SELECT id, parent_exam_id
		      FROM exam
		     WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)
		    UNION ALL
		    SELECT e.id, e.parent_exam_id
		      FROM exam e
		      JOIN ancestors a ON e.id = a.parent_exam_id
		),
		root AS (
		    SELECT TOP 1 id FROM ancestors WHERE parent_exam_id IS NULL
		),
		descendants AS (
		    SELECT id, version FROM exam WHERE id = (SELECT id FROM root)
		    UNION ALL
		    SELECT e.id, e.version
		      FROM exam e
		      JOIN descendants d ON e.parent_exam_id = d.id
		)
		SELECT ISNULL(MAX(version), 0) FROM descendants`
	var v int32
	if err := r.db.QueryRowContext(ctx, q, string(id)).Scan(&v); err != nil {
		return 0, err
	}
	return v, nil
}

func (r *ExamRepo) ExistsByCode(ctx context.Context, code string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM exam WHERE code = @p1`, code).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *ExamRepo) SetPublished(ctx context.Context, id domain.ExamID, published bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE exam SET published = @p1 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		published, string(id))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrExamNotFound
	}
	return nil
}

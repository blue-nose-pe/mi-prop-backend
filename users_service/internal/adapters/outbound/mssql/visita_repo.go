package mssqladapter

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

type VisitaRepo struct {
	db *sql.DB
}

var _ ports.VisitaRepository = (*VisitaRepo)(nil)

func NewVisitaRepo(db *sql.DB) *VisitaRepo { return &VisitaRepo{db: db} }

const visitaCols = `CONVERT(NVARCHAR(36), id),
		CONVERT(NVARCHAR(36), asesor_user_id),
		CONVERT(NVARCHAR(36), school_id),
		scheduled_at, completed_at,
		status,
		ISNULL(notes, ''),
		created_at, updated_at`

func (r *VisitaRepo) Create(ctx context.Context, v *domain.Visita) (domain.VisitaID, error) {
	const q = `
		INSERT INTO visita (asesor_user_id, school_id, scheduled_at, completed_at, status, notes)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1),
		        CONVERT(UNIQUEIDENTIFIER, @p2),
		        @p3,
		        CASE WHEN @p4 = '1900-01-01T00:00:00Z' THEN NULL ELSE @p4 END,
		        @p5,
		        NULLIF(@p6, ''))`
	completed := time.Time{}
	if v.CompletedAt != nil {
		completed = *v.CompletedAt
	} else {
		// sentinel "no completed"
		completed = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	var id string
	err := r.db.QueryRowContext(ctx, q,
		string(v.AsesorUserID), string(v.SchoolID), v.ScheduledAt,
		completed, string(v.Status), v.Notes,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	return domain.VisitaID(id), nil
}

// Update aplica cambios parciales. Convención:
//   - status "" = no tocar
//   - notes "" = no tocar (no se puede vaciar notas con esta API; si hace
//     falta vaciarlas, agregar otro endpoint o sentinel)
//   - scheduled_at zero-value = no tocar
//   - completed_at *nil = no tocar; *zero-time = limpiar a NULL
func (r *VisitaRepo) Update(ctx context.Context, v *domain.Visita) error {
	const q = `
		UPDATE visita
		   SET status        = CASE WHEN @p1 = '' THEN status ELSE @p1 END,
		       notes         = CASE WHEN @p2 = '' THEN notes ELSE @p2 END,
		       scheduled_at  = CASE WHEN @p3 = '0001-01-01T00:00:00Z' THEN scheduled_at ELSE @p3 END,
		       completed_at  = CASE
		                           WHEN @p4 = '0001-01-01T00:00:00Z' THEN completed_at
		                           WHEN @p4 = '1900-01-01T00:00:00Z' THEN NULL
		                           ELSE @p4
		                       END
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p5)`
	completed := time.Time{}
	if v.CompletedAt != nil {
		completed = *v.CompletedAt
	}
	res, err := r.db.ExecContext(ctx, q,
		string(v.Status), v.Notes, v.ScheduledAt, completed, string(v.ID))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrVisitaNotFound
	}
	return nil
}

func (r *VisitaRepo) FindByID(ctx context.Context, id domain.VisitaID) (*domain.Visita, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+visitaCols+` FROM visita WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(id))
	var (
		v           domain.Visita
		idStr       string
		asesor      string
		school      string
		completedAt sql.NullTime
		statusStr   string
		updatedAt   sql.NullTime
	)
	err := row.Scan(&idStr, &asesor, &school, &v.ScheduledAt, &completedAt, &statusStr, &v.Notes, &v.CreatedAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrVisitaNotFound
	}
	if err != nil {
		return nil, err
	}
	v.ID = domain.VisitaID(idStr)
	v.AsesorUserID = domain.UserID(asesor)
	v.SchoolID = domain.SchoolID(school)
	v.Status = domain.VisitaStatus(statusStr)
	if completedAt.Valid {
		t := completedAt.Time
		v.CompletedAt = &t
	}
	if updatedAt.Valid {
		t := updatedAt.Time
		v.UpdatedAt = &t
	}
	return &v, nil
}

func (r *VisitaRepo) List(ctx context.Context, in ports.ListVisitasInput) ([]domain.Visita, uint32, error) {
	if in.Limit == 0 || in.Limit > 1000 {
		in.Limit = 100
	}
	where := "WHERE 1=1"
	args := []any{}
	idx := 1
	if in.AsesorUserID != "" {
		where += " AND asesor_user_id = CONVERT(UNIQUEIDENTIFIER, @p" + strconv.Itoa(idx) + ")"
		args = append(args, string(in.AsesorUserID))
		idx++
	}
	if in.SchoolID != "" {
		where += " AND school_id = CONVERT(UNIQUEIDENTIFIER, @p" + strconv.Itoa(idx) + ")"
		args = append(args, string(in.SchoolID))
		idx++
	}
	if in.Status != "" {
		where += " AND status = @p" + strconv.Itoa(idx)
		args = append(args, string(in.Status))
		idx++
	}
	if in.From != nil {
		where += " AND scheduled_at >= @p" + strconv.Itoa(idx)
		args = append(args, *in.From)
		idx++
	}
	if in.To != nil {
		where += " AND scheduled_at < @p" + strconv.Itoa(idx)
		args = append(args, *in.To)
		idx++
	}

	var total uint32
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM visita "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Ordenar por la fecha REAL de la visita: completed_at si existe (visita
	// realizada), si no scheduled_at. Antes era solo scheduled_at, lo que hacía
	// que el page de "completadas" se eligiera por fecha programada y el front
	// (que ordena por completed_at) mostrara un orden inconsistente. COALESCE
	// mantiene el orden por scheduled_at para las que aún no tienen completed_at.
	q := `SELECT ` + visitaCols + `
	        FROM visita ` + where + `
	       ORDER BY COALESCE(completed_at, scheduled_at) DESC
	      OFFSET @p` + strconv.Itoa(idx) + ` ROWS FETCH NEXT @p` + strconv.Itoa(idx+1) + ` ROWS ONLY`
	args = append(args, in.Offset, in.Limit)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]domain.Visita, 0)
	for rows.Next() {
		var (
			v           domain.Visita
			idStr       string
			asesor      string
			school      string
			completedAt sql.NullTime
			statusStr   string
			updatedAt   sql.NullTime
		)
		if err := rows.Scan(&idStr, &asesor, &school, &v.ScheduledAt, &completedAt, &statusStr, &v.Notes, &v.CreatedAt, &updatedAt); err != nil {
			return nil, 0, err
		}
		v.ID = domain.VisitaID(idStr)
		v.AsesorUserID = domain.UserID(asesor)
		v.SchoolID = domain.SchoolID(school)
		v.Status = domain.VisitaStatus(statusStr)
		if completedAt.Valid {
			t := completedAt.Time
			v.CompletedAt = &t
		}
		if updatedAt.Valid {
			t := updatedAt.Time
			v.UpdatedAt = &t
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

func (r *VisitaRepo) CountByAsesorStatus(ctx context.Context, asesorID domain.UserID, status domain.VisitaStatus) (int32, error) {
	var n int32
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM visita
		  WHERE asesor_user_id = CONVERT(UNIQUEIDENTIFIER, @p1) AND status = @p2`,
		string(asesorID), string(status)).Scan(&n)
	return n, err
}

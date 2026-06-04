package mssqladapter

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

type SchoolRepo struct {
	db *sql.DB
}

var _ ports.SchoolRepository = (*SchoolRepo)(nil)

func NewSchoolRepo(db *sql.DB) *SchoolRepo { return &SchoolRepo{db: db} }

func (r *SchoolRepo) FindByID(ctx context.Context, id domain.SchoolID) (*domain.School, error) {
	// Cliente: code + penetration vienen del commit f96164d (migration 023).
	// El SELECT de FindByID se olvidó incluirlos mientras el Scan abajo SI los
	// lee — eso causaba "Internal" en GetSchool por column count mismatch.
	// Resultado visible: el detalle del colegio mostraba cards Ciudad /
	// Segmento / Key de acceso vacias (front recibia error y caia al else).
	const q = `SELECT CONVERT(NVARCHAR(36), id),
	                  int_id,
	                  CONVERT(NVARCHAR(36), user_id),
	                  name, ISNULL(city, ''), ISNULL(category, ''),
	                  ISNULL(code, ''), ISNULL(penetration, ''),
	                  active, created_at, updated_at,
	                  ISNULL(hubspot_record_id, ''),
	                  ISNULL(email, ''), ISNULL(phone, '')
	             FROM school WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`

	var (
		s         domain.School
		idStr     string
		userIDStr string
		updatedAt sql.NullTime
		hubspotID string
	)
	err := r.db.QueryRowContext(ctx, q, string(id)).
		Scan(&idStr, &s.IntID, &userIDStr, &s.Name, &s.City, &s.Category, &s.Code, &s.Penetration, &s.Active, &s.CreatedAt, &updatedAt, &hubspotID, &s.Email, &s.Phone)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrSchoolNotFound
	}
	if err != nil {
		return nil, err
	}
	s.ID = domain.SchoolID(idStr)
	s.UserID = domain.UserID(userIDStr)
	s.HubspotRecordID = hubspotID
	if updatedAt.Valid {
		t := updatedAt.Time
		s.UpdatedAt = &t
	}
	return &s, nil
}

// List devuelve schools paginados, opcionalmente filtrando por nombre
// (LIKE %search%, case-insensitive via COLLATE) y/o solo activos. Devuelve
// también el total sin paginar para que la UI pueda armar paginación.
func (r *SchoolRepo) List(ctx context.Context, in ports.ListSchoolsInput) ([]domain.School, uint32, error) {
	if in.Limit == 0 || in.Limit > 1000 {
		in.Limit = 100
	}
	where := "WHERE 1=1"
	args := []any{}
	idx := 1
	if in.ActiveOnly {
		where += " AND active = 1"
	}
	if in.Search != "" {
		where += " AND name COLLATE Latin1_General_CI_AI LIKE @p" + strconv.Itoa(idx)
		args = append(args, "%"+in.Search+"%")
		idx++
	}

	var total uint32
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM school "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	q := `SELECT CONVERT(NVARCHAR(36), id),
	             ISNULL(CONVERT(NVARCHAR(36), user_id), ''),
	             name, ISNULL(city, ''), ISNULL(category, ''),
	             ISNULL(code, ''), ISNULL(penetration, ''),
	             active, created_at, updated_at,
	             ISNULL(hubspot_record_id, ''),
	             ISNULL(email, ''), ISNULL(phone, '')
	        FROM school ` + where + `
	       ORDER BY name ASC
	      OFFSET @p` + strconv.Itoa(idx) + ` ROWS FETCH NEXT @p` + strconv.Itoa(idx+1) + ` ROWS ONLY`
	args = append(args, in.Offset, in.Limit)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]domain.School, 0)
	for rows.Next() {
		var (
			s         domain.School
			idStr     string
			userIDStr string
			updatedAt sql.NullTime
			hubspotID string
		)
		if err := rows.Scan(&idStr, &userIDStr, &s.Name, &s.City, &s.Category, &s.Code, &s.Penetration, &s.Active, &s.CreatedAt, &updatedAt, &hubspotID, &s.Email, &s.Phone); err != nil {
			return nil, 0, err
		}
		s.ID = domain.SchoolID(idStr)
		s.UserID = domain.UserID(userIDStr)
		s.HubspotRecordID = hubspotID
		if updatedAt.Valid {
			t := updatedAt.Time
			s.UpdatedAt = &t
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

// Create persiste un colegio nuevo. La BD genera el UNIQUEIDENTIFIER vía
// DEFAULT NEWID() y lo recuperamos con OUTPUT INSERTED.id.
func (r *SchoolRepo) Create(ctx context.Context, s *domain.School) (domain.SchoolID, error) {
	const q = `
		INSERT INTO school (user_id, name, city, category, code, penetration, active, hubspot_record_id, email, phone)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (CONVERT(UNIQUEIDENTIFIER, @p1), @p2,
		        NULLIF(@p3, ''), NULLIF(@p4, ''),
		        NULLIF(@p5, ''), NULLIF(@p6, ''),
		        @p7, NULLIF(@p8, ''),
		        NULLIF(@p9, ''), NULLIF(@p10, ''))`
	var id string
	err := r.db.QueryRowContext(ctx, q,
		string(s.UserID), s.Name, s.City, s.Category, s.Code, s.Penetration, s.Active, s.HubspotRecordID, s.Email, s.Phone,
	).Scan(&id)
	if err != nil {
		return "", err
	}
	return domain.SchoolID(id), nil
}

// Update aplica cambios parciales (campos vacíos no se tocan).
//
// Para limpiar city o category hay que mandar el sentinel "-" en el campo,
// no "": el "" significa "no tocar". Esto sigue la misma convención que
// hubspot_record_id en este repo.
func (r *SchoolRepo) Update(ctx context.Context, s *domain.School) error {
	const q = `
		UPDATE school
		   SET name              = COALESCE(NULLIF(@p1, ''), name),
		       user_id           = COALESCE(IIF(@p2 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p2)), user_id),
		       hubspot_record_id = COALESCE(NULLIF(@p3, ''), hubspot_record_id),
		       city              = CASE WHEN @p4 = '' THEN city
		                                WHEN @p4 = '-' THEN NULL
		                                ELSE @p4 END,
		       category          = CASE WHEN @p5 = '' THEN category
		                                WHEN @p5 = '-' THEN NULL
		                                ELSE @p5 END,
		       code              = CASE WHEN @p6 = '' THEN code
		                                WHEN @p6 = '-' THEN NULL
		                                ELSE @p6 END,
		       penetration       = CASE WHEN @p7 = '' THEN penetration
		                                WHEN @p7 = '-' THEN NULL
		                                ELSE @p7 END,
		       email             = CASE WHEN @p9 = '' THEN email
		                                WHEN @p9 = '-' THEN NULL
		                                ELSE @p9 END,
		       phone             = CASE WHEN @p10 = '' THEN phone
		                                WHEN @p10 = '-' THEN NULL
		                                ELSE @p10 END
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p8)`
	res, err := r.db.ExecContext(ctx, q,
		s.Name, string(s.UserID), s.HubspotRecordID, s.City, s.Category, s.Code, s.Penetration, string(s.ID), s.Email, s.Phone)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrSchoolNotFound
	}
	return nil
}

// ListByAsesor: JOIN con assignment para resolver colegios del asesor en
// un solo viaje a la DB. assignment.target_user_id apunta al usuario
// coordinador del colegio; school.user_id = ese mismo coordinator.
func (r *SchoolRepo) ListByAsesor(ctx context.Context, asesorID domain.UserID) ([]domain.School, error) {
	const q = `
		SELECT CONVERT(NVARCHAR(36), s.id),
		       ISNULL(CONVERT(NVARCHAR(36), s.user_id), ''),
		       s.name, ISNULL(s.city, ''), ISNULL(s.category, ''),
		       ISNULL(s.code, ''), ISNULL(s.penetration, ''),
		       s.active, s.created_at, s.updated_at,
		       ISNULL(s.hubspot_record_id, ''),
		       ISNULL(s.email, ''), ISNULL(s.phone, '')
		  FROM school s
		  JOIN assignment a ON a.target_user_id = s.user_id
		 WHERE a.source_user_id = CONVERT(UNIQUEIDENTIFIER, @p1)
		   AND a.kind = 'asesor_de_colegio'
		   AND a.valid_to IS NULL
		 ORDER BY s.name`
	rows, err := r.db.QueryContext(ctx, q, string(asesorID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.School, 0)
	for rows.Next() {
		var (
			s         domain.School
			idStr     string
			userIDStr string
			updatedAt sql.NullTime
			hubspotID string
		)
		if err := rows.Scan(&idStr, &userIDStr, &s.Name, &s.City, &s.Category, &s.Code, &s.Penetration, &s.Active, &s.CreatedAt, &updatedAt, &hubspotID, &s.Email, &s.Phone); err != nil {
			return nil, err
		}
		s.ID = domain.SchoolID(idStr)
		s.UserID = domain.UserID(userIDStr)
		s.HubspotRecordID = hubspotID
		if updatedAt.Valid {
			t := updatedAt.Time
			s.UpdatedAt = &t
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetHubspotRecordID guarda el record_id que devuelve HubSpot al crear
// un colegio (custom object o company). Lo invoca el hubspot-service vía
// gRPC tras un sync exitoso.
func (r *SchoolRepo) SetHubspotRecordID(ctx context.Context, id domain.SchoolID, recordID string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE school SET hubspot_record_id = NULLIF(@p1, '')
		  WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		recordID, string(id))
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrSchoolNotFound
	}
	return nil
}

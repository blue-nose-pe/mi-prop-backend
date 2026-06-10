// Package mssqladapter implementa los puertos outbound de satisfaction_service.
package mssqladapter

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	mssql "github.com/microsoft/go-mssqldb"

	"satisfaction_service/internal/core/domain"
	"satisfaction_service/internal/core/ports"
)

type SurveyRepo struct {
	db *sql.DB
	*SearchEngine
}

var _ ports.SurveyRepository = (*SurveyRepo)(nil)

func NewSurveyRepo(db *sql.DB) *SurveyRepo {
	return &SurveyRepo{db: db, SearchEngine: NewSearchEngine(db, surveySearchSchema)}
}

const surveyCols = `CONVERT(NVARCHAR(36), id),
		code,
		title,
		ISNULL(description, ''),
		target_role,
		trigger_kind,
		version,
		published,
		active,
		created_at,
		updated_at,
		ISNULL(CONVERT(NVARCHAR(36), key_id), '')`

func (r *SurveyRepo) Save(ctx context.Context, s *domain.Survey) (domain.SurveyID, error) {
	const q = `
		INSERT INTO survey (code, title, description, target_role, trigger_kind, version, published, active, key_id)
		OUTPUT CONVERT(NVARCHAR(36), INSERTED.id)
		VALUES (@p1, @p2, NULLIF(@p3, ''), @p4, @p5, @p6, @p7, @p8,
		        IIF(@p9 = '', NULL, CONVERT(UNIQUEIDENTIFIER, @p9)))`
	var id string
	err := r.db.QueryRowContext(ctx, q,
		s.Code, s.Title, s.Description, s.TargetRole, s.Trigger,
		s.Version, s.Published, s.Active, s.KeyID,
	).Scan(&id)
	if err != nil {
		return "", mapDuplicate(err)
	}
	return domain.SurveyID(id), nil
}

func (r *SurveyRepo) Update(ctx context.Context, s *domain.Survey) error {
	// Cliente: trigger_kind editable post-creacion. Persistimos con NULLIF
	// para que "" -> NULL (consistente con como se persiste al crear).
	const q = `
		UPDATE survey
		   SET title        = @p1,
		       description  = NULLIF(@p2, ''),
		       trigger_kind = NULLIF(@p3, ''),
		       key_id       = CASE WHEN @p5 = '' THEN key_id
		                           WHEN @p5 = '-' THEN NULL
		                           ELSE CONVERT(UNIQUEIDENTIFIER, @p5) END
		 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p4)`
	res, err := r.db.ExecContext(ctx, q, s.Title, s.Description, s.Trigger, string(s.ID), s.KeyID)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrSurveyNotFound
	}
	return nil
}

func (r *SurveyRepo) FindByID(ctx context.Context, id domain.SurveyID) (*domain.Survey, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+surveyCols+` FROM survey WHERE id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(id))
	return scanSurvey(row)
}

func (r *SurveyRepo) FindByCodeVersion(ctx context.Context, code string, version int32) (*domain.Survey, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+surveyCols+` FROM survey WHERE code = @p1 AND version = @p2`,
		code, version)
	return scanSurvey(row)
}

// FindActivePublished: encuesta publicada+activa aplicable para una key.
//
// Modelo key -> survey (tabla survey_key, migration 007): una key usa como
// mucho UNA encuesta; una encuesta sirve a MUCHAS keys.
//   1) Si la key tiene una encuesta asignada (survey_key) -> esa (prioridad).
//   2) Si no, cae a la GENERAL por trigger_kind: una encuesta publicada de ese
//      tipo que NO este asignada a ninguna key (equivalente al viejo key_id
//      NULL). Asi una encuesta key-especifica nunca actua de general.
//
// Comparamos la key como string (CONVERT a NVARCHAR) para no convertir @p2 a
// UNIQUEIDENTIFIER cuando viene "" (evita error de conversion).
func (r *SurveyRepo) FindActivePublished(ctx context.Context, triggerKind, keyID string) (*domain.Survey, error) {
	const q = `SELECT TOP 1 ` + surveyCols + `
		FROM survey s
		WHERE s.published = 1 AND s.active = 1
		  AND (
		        s.id = (SELECT TOP 1 sk.survey_id FROM survey_key sk
		                 WHERE @p2 <> '' AND CONVERT(NVARCHAR(36), sk.key_id) = @p2)
		     OR (
		          NOT EXISTS (SELECT 1 FROM survey_key sk
		                       WHERE @p2 <> '' AND CONVERT(NVARCHAR(36), sk.key_id) = @p2)
		          AND s.trigger_kind = @p1
		          AND NOT EXISTS (SELECT 1 FROM survey_key sk2 WHERE sk2.survey_id = s.id)
		        )
		  )
		ORDER BY
		  CASE WHEN s.id = (SELECT TOP 1 sk.survey_id FROM survey_key sk
		                     WHERE @p2 <> '' AND CONVERT(NVARCHAR(36), sk.key_id) = @p2)
		       THEN 0 ELSE 1 END,
		  s.version DESC`
	row := r.db.QueryRowContext(ctx, q, triggerKind, keyID)
	return scanSurvey(row)
}

// AssignKeyToSurvey: upsert (key_id -> survey_id) en survey_key. key_id es PK,
// asi que re-asignar una key a otra encuesta la MUEVE (cambia su survey_id);
// pero asignar la MISMA encuesta a varias keys crea filas distintas (la
// encuesta sirve a muchas keys). keyID viene como string uuid.
func (r *SurveyRepo) AssignKeyToSurvey(ctx context.Context, surveyID domain.SurveyID, keyID string) error {
	const q = `
		MERGE dbo.survey_key AS tgt
		USING (SELECT CONVERT(UNIQUEIDENTIFIER, @p1) AS key_id,
		              CONVERT(UNIQUEIDENTIFIER, @p2) AS survey_id) AS src
		   ON tgt.key_id = src.key_id
		 WHEN MATCHED THEN UPDATE SET survey_id = src.survey_id
		 WHEN NOT MATCHED THEN INSERT (key_id, survey_id) VALUES (src.key_id, src.survey_id);`
	_, err := r.db.ExecContext(ctx, q, keyID, string(surveyID))
	return err
}

func (r *SurveyRepo) SetPublished(ctx context.Context, id domain.SurveyID, published bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE survey SET published = @p1 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		published, string(id))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrSurveyNotFound
	}
	return nil
}

func (r *SurveyRepo) SetActive(ctx context.Context, id domain.SurveyID, active bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE survey SET active = @p1 WHERE id = CONVERT(UNIQUEIDENTIFIER, @p2)`,
		active, string(id))
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return domain.ErrSurveyNotFound
	}
	return nil
}

func mapDuplicate(err error) error {
	var msErr mssql.Error
	if errors.As(err, &msErr) && (msErr.Number == 2627 || msErr.Number == 2601) {
		if strings.Contains(strings.ToLower(msErr.Message), "uk_survey_code_version") {
			return domain.ErrSurveyCodeTaken
		}
	}
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSurvey(s rowScanner) (*domain.Survey, error) {
	var (
		sv        domain.Survey
		idStr     string
		updatedAt sql.NullTime
	)
	err := s.Scan(
		&idStr, &sv.Code, &sv.Title, &sv.Description,
		&sv.TargetRole, &sv.Trigger, &sv.Version,
		&sv.Published, &sv.Active, &sv.CreatedAt, &updatedAt,
		&sv.KeyID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrSurveyNotFound
	}
	if err != nil {
		return nil, err
	}
	sv.ID = domain.SurveyID(idStr)
	if updatedAt.Valid {
		v := updatedAt.Time
		sv.UpdatedAt = &v
	}
	return &sv, nil
}

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

// attachedKeyExpr proyecta la llave REALMENTE atada a la encuesta desde la
// tabla survey_key (migration 007), NO la columna legacy survey.key_id (que
// desde ese modelo queda NULL). El subselect correlaciona con la tabla base
// `survey` sin alias, así que las queries que lo usan deben referenciar la
// tabla como `survey` (no `s`). Como el modelo es 1 encuesta -> N llaves,
// tomamos la primera (hoy todas son 1:1); el reporte suma por todas las
// llaves via el gateway.
const attachedKeyExpr = `ISNULL((SELECT TOP 1 CONVERT(NVARCHAR(36), sk.key_id)
		FROM survey_key sk WHERE sk.survey_id = survey.id), '')`

const surveyCols = `CONVERT(NVARCHAR(36), survey.id),
		survey.code,
		survey.title,
		ISNULL(survey.description, ''),
		survey.target_role,
		survey.trigger_kind,
		survey.version,
		survey.published,
		survey.active,
		survey.created_at,
		survey.updated_at,
		` + attachedKeyExpr

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
	// Usamos la tabla SIN alias (`FROM survey`) porque surveyCols/attachedKeyExpr
	// correlacionan con `survey.id`. El fallback a la general (branch 2) solo se
	// bloquea si la encuesta asignada a la key está VIVA (published+active): si
	// se pausa/despublica, la key cae a la general de su tipo en vez de quedarse
	// sin encuesta (fix auditoría 2026-07-03). Desempate determinista por
	// created_at/id para no depender del plan de SQL Server con versiones iguales.
	const q = `SELECT TOP 1 ` + surveyCols + `
		FROM survey
		WHERE survey.published = 1 AND survey.active = 1
		  AND (
		        survey.id = (SELECT TOP 1 sk.survey_id FROM survey_key sk
		                 WHERE @p2 <> '' AND CONVERT(NVARCHAR(36), sk.key_id) = @p2)
		     OR (
		          NOT EXISTS (SELECT 1 FROM survey_key sk
		                       JOIN survey sa ON sa.id = sk.survey_id
		                       WHERE @p2 <> '' AND CONVERT(NVARCHAR(36), sk.key_id) = @p2
		                         AND sa.published = 1 AND sa.active = 1)
		          AND survey.trigger_kind = @p1
		          AND NOT EXISTS (SELECT 1 FROM survey_key sk2 WHERE sk2.survey_id = survey.id)
		        )
		  )
		ORDER BY
		  CASE WHEN survey.id = (SELECT TOP 1 sk.survey_id FROM survey_key sk
		                     WHERE @p2 <> '' AND CONVERT(NVARCHAR(36), sk.key_id) = @p2)
		       THEN 0 ELSE 1 END,
		  survey.version DESC, survey.created_at DESC, survey.id`
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

// UnassignKeysFromSurvey elimina TODAS las filas survey_key de esta encuesta —
// la vuelve "general por tipo de examen" (el '-'/"Todas" del builder). Sin
// esto, "limpiar" el targeting en la UI era un no-op y la encuesta seguía
// atada a su llave para siempre.
func (r *SurveyRepo) UnassignKeysFromSurvey(ctx context.Context, surveyID domain.SurveyID) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM dbo.survey_key WHERE survey_id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(surveyID))
	return err
}

// HasKeys indica si la encuesta está atada a al menos una llave (survey_key).
// Se usa al publicar: una encuesta con trigger no-tipo (recurring/legacy) SÍ
// puede publicarse si está dirigida a una llave concreta.
func (r *SurveyRepo) HasKeys(ctx context.Context, surveyID domain.SurveyID) (bool, error) {
	var x int
	err := r.db.QueryRowContext(ctx,
		`SELECT TOP 1 1 FROM dbo.survey_key WHERE survey_id = CONVERT(UNIQUEIDENTIFIER, @p1)`,
		string(surveyID)).Scan(&x)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
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

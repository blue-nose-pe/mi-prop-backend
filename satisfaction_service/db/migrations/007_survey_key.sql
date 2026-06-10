-- ====================================================================
-- Cliente (2026-06-10) — una encuesta puede servir a MUCHAS keys.
--
-- El modelo previo (survey.key_id, migration 006) ataba una encuesta a UNA
-- sola key: re-asignar la misma encuesta a otra key sobreescribia la anterior
-- (last-write-wins). El cliente necesita lo inverso: elegir la MISMA encuesta
-- al crear varias keys (p.ej. 3 keys con la encuesta A, 2 con la B).
--
-- Invertimos la relacion a key -> survey con una tabla puente:
--   * survey_key.key_id es PK  -> una key usa como MUCHO una encuesta.
--   * survey_key.survey_id puede repetirse -> una encuesta sirve a muchas keys.
--
-- key_id/survey_id referencian logicamente db_keys.[key].id y survey.id; sin FK
-- cross-database. La validez se resuelve en la capa de aplicacion.
--
-- Migramos las asignaciones existentes de survey.key_id (006) a la tabla nueva.
-- survey.key_id queda como columna legacy (ya no se usa para resolver).
--
-- Idempotente: IF NOT EXISTS sobre sys.tables + NOT EXISTS en el INSERT.
-- ====================================================================

SET QUOTED_IDENTIFIER ON;
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.tables WHERE name = 'survey_key' AND schema_id = SCHEMA_ID('dbo')
)
    CREATE TABLE dbo.survey_key (
        key_id     UNIQUEIDENTIFIER NOT NULL PRIMARY KEY,
        survey_id  UNIQUEIDENTIFIER NOT NULL,
        created_at DATETIME2        NOT NULL DEFAULT SYSUTCDATETIME()
    );
GO

SET QUOTED_IDENTIFIER ON;
GO

-- Index para resolver rapido "que encuestas usan la key X" / "a que keys
-- esta asignada la encuesta Y".
IF NOT EXISTS (
    SELECT 1 FROM sys.indexes
     WHERE name = 'idx_survey_key_survey_id' AND object_id = OBJECT_ID('dbo.survey_key')
)
    CREATE INDEX idx_survey_key_survey_id ON dbo.survey_key (survey_id);
GO

SET QUOTED_IDENTIFIER ON;
GO

-- Migrar asignaciones existentes (survey.key_id -> survey_key). Solo las que
-- aun no estan en la tabla nueva (re-ejecutable).
INSERT INTO dbo.survey_key (key_id, survey_id)
SELECT s.key_id, s.id
  FROM dbo.survey s
 WHERE s.key_id IS NOT NULL
   AND NOT EXISTS (
       SELECT 1 FROM dbo.survey_key sk WHERE sk.key_id = s.key_id
   );
GO

-- ====================================================================
-- Cliente (2026-06-04) — una encuesta puede dirigirse a una KEY especifica
-- (no solo por tipo de examen via trigger_kind). Agregamos key_id nullable.
--
--   key_id NULL  -> la encuesta aplica por trigger_kind (comportamiento previo).
--   key_id != NULL -> la encuesta es solo para estudiantes que rindieron con
--                     esa key.
--
-- key_id referencia logicamente a db_keys.dbo.[key].id, pero NO ponemos FK:
-- es una tabla en OTRA base (cross-database FK no es soportado). La validez
-- se resuelve en la capa de aplicacion.
--
-- Idempotente — IF NOT EXISTS sobre sys.columns. Re-ejecutable.
-- ====================================================================

SET QUOTED_IDENTIFIER ON;
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('dbo.survey') AND name = 'key_id'
)
    ALTER TABLE dbo.survey ADD key_id UNIQUEIDENTIFIER NULL;
GO

SET QUOTED_IDENTIFIER ON;
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.indexes
     WHERE name = 'idx_survey_key_id' AND object_id = OBJECT_ID('dbo.survey')
)
    CREATE INDEX idx_survey_key_id ON dbo.survey (key_id) WHERE key_id IS NOT NULL;
GO

-- ====================================================================
-- Auditoría 2026-07-03 — backstop de integridad contra respuestas duplicadas.
--
-- La dedup por código (command/response.go Submit) evita el caso normal, pero
-- no cubre la carrera de doble-click de ~0.7s (dos INSERT casi simultáneos que
-- ambos pasan el check-then-insert). Este índice único FILTRADO es el cinturón:
-- una encuesta no puede tener dos respuestas del mismo intento de examen.
--
-- Filtramos por exam_attempt_id IS NOT NULL para no romper el flujo anónimo
-- sin intento ni las encuestas 'recurring' (varias respuestas a propósito, sin
-- intento). Verificado antes de crear: 0 violaciones en datos existentes.
--
-- Idempotente: IF NOT EXISTS evita recrearlo.
-- ====================================================================

SET QUOTED_IDENTIFIER ON;

IF NOT EXISTS (
    SELECT 1 FROM sys.indexes
    WHERE name = 'uq_survey_response_survey_attempt'
      AND object_id = OBJECT_ID('dbo.survey_response')
)
    CREATE UNIQUE INDEX uq_survey_response_survey_attempt
        ON dbo.survey_response (survey_id, exam_attempt_id)
        WHERE exam_attempt_id IS NOT NULL;

-- ====================================================================
-- Cliente (2026-06-17) — encuesta de satisfacción PÚBLICA / anónima.
--
-- Las encuestas publicadas se exponen sin sesión en /api/public/* (el alumno
-- anónimo responde la satisfacción tras un examen masivo). En ese flujo el
-- user_id viene vacío, pero survey_response.user_id era UNIQUEIDENTIFIER
-- NOT NULL y el INSERT hacía CONVERT(UNIQUEIDENTIFIER, '') → error → 500.
--
-- La hacemos NULLABLE para permitir respuestas anónimas. La dedup por usuario
-- (ExistsByUserSurvey/Attempt) ya no se aplica cuando no hay user_id (guard en
-- el handler Submit). El índice (user_id, submitted_at) admite NULLs.
--
-- Idempotente — re-ejecutable: ALTER COLUMN a NULL es no-op si ya es nullable.
-- ====================================================================

SET QUOTED_IDENTIFIER ON;

ALTER TABLE survey_response ALTER COLUMN user_id UNIQUEIDENTIFIER NULL;

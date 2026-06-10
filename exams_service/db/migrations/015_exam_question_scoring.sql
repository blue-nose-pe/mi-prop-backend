-- ====================================================================
-- Cliente (2026-06-10) — scoring fiel a producción v1 del simulacro.
--
-- En prod cada módulo del simulacro tiene su lógica de puntaje:
--   puntaje_pregunta_correcta / _incorrecta / _enblanco.
--   p.ej. UCSP: correcta +5, incorrecta 0, EN BLANCO +1 (¡el blanco SUMA!).
--
-- v2 solo tenía `points` (puntos si correcta). Agregamos los otros dos para
-- que el motor (attempt.Finish) replique el cálculo exacto:
--   score = Σ (correctas·points + incorrectas·points_incorrect + blancos·points_blank)
-- `points` sigue siendo el puntaje por CORRECTA. v2 no tiene módulos: cada
-- pregunta lleva su propio trío (heredado del módulo de origen al importar).
--
-- Idempotente — IF NOT EXISTS sobre sys.columns.
-- ====================================================================

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('exam_question') AND name = 'points_incorrect')
    ALTER TABLE exam_question ADD points_incorrect INT NOT NULL CONSTRAINT df_exam_question_points_incorrect DEFAULT 0;

IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('exam_question') AND name = 'points_blank')
    ALTER TABLE exam_question ADD points_blank INT NOT NULL CONSTRAINT df_exam_question_points_blank DEFAULT 0;

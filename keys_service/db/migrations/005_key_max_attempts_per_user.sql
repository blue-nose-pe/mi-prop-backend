-- Agrega max_attempts_per_user a [key]: cuantas veces UN MISMO estudiante
-- puede rendir la prueba ligada a esta key. Antes el back solo limitaba
-- "plazas totales" via max_uses (aforo), pero un mismo alumno podia
-- rendir varias veces consumiendo plazas extra.
--
-- Default = 1 (un alumno = un intento). Si el asesor quiere permitir
-- multi-intento (caso "practica"), lo sube al crear/editar la key.
-- max_attempts_per_user = 0 → ilimitado (no recomendado, puede vaciar
-- el aforo con un solo alumno).
--
-- Enforcement vive en exams_service.StartAttempt: cuenta attempts con
-- submitted_at IS NOT NULL del (user_id, exam_id) y si llega al max,
-- rechaza con MAX_ATTEMPTS_REACHED y devuelve el ultimo attempt para
-- que el front mande al alumno a /resultados.

SET QUOTED_IDENTIFIER ON;
SET ANSI_NULLS ON;
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'[dbo].[key]') AND name = 'max_attempts_per_user'
)
BEGIN
    ALTER TABLE [key]
        ADD max_attempts_per_user INT NOT NULL CONSTRAINT DF_key_max_attempts_per_user DEFAULT 1;
END;
GO

-- Agrega duration_minutes a exam_attempt: el límite de tiempo del intento,
-- LOCKEADO al iniciar (denormalizado desde el exam según tipo/código en
-- command.Start.durationMinutesFor). Hace el timer REAL (server-side): el back
-- rechaza respuestas que llegan pasado started_at + duration_minutes + gracia.
--
-- 0 = sin límite (vocacional/estilos, que son tests de tendencia). Simulacro:
-- UCSP = 150 min, Nacional (SIME-NAC-*) = 180 min (igual que prod/front).
--
-- Default 0 para los intentos viejos (ya finalizados, no se les aplica nada).

SET QUOTED_IDENTIFIER ON;
SET ANSI_NULLS ON;
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'[dbo].[exam_attempt]') AND name = 'duration_minutes'
)
BEGIN
    ALTER TABLE exam_attempt
        ADD duration_minutes INT NOT NULL CONSTRAINT DF_exam_attempt_duration DEFAULT 0;
END;
GO

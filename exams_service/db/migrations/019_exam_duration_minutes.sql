-- B5: timer configurable POR EXAMEN. Agrega exam.duration_minutes INT NULL.
--
-- NULL = usar el default por tipo (command.durationMinutesFor: simulacro UCSP
-- 150, Nacional SIME-NAC-* 180, vocacional/estilos 0). Si está seteado, ESE
-- valor (en minutos) manda al iniciar el intento; 0 explícito = sin límite.
--
-- No toca los exámenes existentes (quedan en NULL = comportamiento previo por
-- tipo). El admin puede setearlo desde el editor ("Tiempo límite (minutos)").

SET QUOTED_IDENTIFIER ON;
SET ANSI_NULLS ON;
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'[dbo].[exam]') AND name = 'duration_minutes'
)
BEGIN
    ALTER TABLE exam ADD duration_minutes INT NULL;
END
GO

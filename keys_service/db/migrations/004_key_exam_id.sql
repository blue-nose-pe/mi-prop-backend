-- Agrega exam_id (opcional) a [key] para fijar la version exacta del exam
-- que la key va a servir. Antes la key apuntaba solo a exam_type_id y el
-- front buscaba "primer exam publicado del tipo" — no-deterministico si
-- UCSP tenia 2 versiones publicadas a la vez.
--
-- Nullable: keys preexistentes siguen funcionando con el fallback
-- type-based en el front (loadQuestionsForKey). Cuando exam_id != NULL,
-- el front carga ese exam puntual (deterministico).
--
-- NO hay FK fisica a db_exams.exam porque Azure SQL no soporta FK
-- cross-database. La integridad se valida a nivel servicio (keys_service
-- no valida que el exam exista; exams_service rechaza StartAttempt con
-- EXAM_NOT_FOUND si el id no existe).

-- Filtered index requiere QUOTED_IDENTIFIER + ANSI_NULLS = ON. sqlcmd los
-- pone OFF por default; explicitamos para que la migracion sea idempotente
-- corriendo desde sqlcmd, ssms y como pre-install Helm hook.
SET QUOTED_IDENTIFIER ON;
SET ANSI_NULLS ON;
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'[dbo].[key]') AND name = 'exam_id'
)
BEGIN
    ALTER TABLE [key] ADD exam_id UNIQUEIDENTIFIER NULL;
END;
GO

-- Indice filtrado: solo keys con exam_id apuntando a una version puntual.
-- Acelera "todas las keys ligadas a este exam" cuando UCSP retire una version.
-- Se ejecuta en batch separado (GO) porque SQL Server parsea CREATE INDEX
-- en compile-time y necesita ver la columna recien creada.
IF NOT EXISTS (
    SELECT 1 FROM sys.indexes WHERE name = 'idx_key_exam' AND object_id = OBJECT_ID(N'[dbo].[key]')
)
BEGIN
    EXEC('CREATE INDEX idx_key_exam ON [key](exam_id) WHERE exam_id IS NOT NULL');
END;

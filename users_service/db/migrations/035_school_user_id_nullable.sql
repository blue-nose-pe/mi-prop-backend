-- ====================================================================
-- Cliente (C4) — "Poder ELIMINAR el coordinador de un colegio" (queda sin
-- coordinador, conservando toda su info hasta reasignar uno).
--
-- (1) school.user_id (coordinador/owner) era NOT NULL. Para poder quitarlo
--     (UpdateSchool con user_id="-" → NULL) la columna debe permitir NULL.
--     CreateSchool sigue exigiendo user_id (un colegio nuevo nace con
--     coordinador); solo el update puede dejarlo en NULL. El back ya lee NULL
--     con ISNULL (school_repo.FindByID) y schoolToProto lo mapea a "".
--
-- (2) uk_school_user era un UNIQUE CONSTRAINT NO filtrado sobre user_id. Un
--     unique no-filtrado en SQL Server permite UN SOLO NULL → si se quitara el
--     coordinador a DOS colegios a la vez, el 2do NULL colisionaría. Lo
--     convertimos a UNIQUE INDEX FILTRADO (WHERE user_id IS NOT NULL): sigue
--     garantizando 1 coordinador = 1 colegio entre los asignados, pero permite
--     VARIOS colegios sin coordinador. (Verificado: no hay FK que lo referencie.)
--
-- NOTA: un índice filtrado obliga QUOTED_IDENTIFIER ON en los writes a la tabla.
-- go-mssqldb (driver de la app) lo setea ON por defecto — verificado en vivo que
-- los updates de colegio siguen dando 200. El SET de abajo es por las dudas para
-- el runner de migraciones.
--
-- Idempotente — guards sobre is_nullable y sys.key_constraints. Re-ejecutable.
-- ====================================================================
SET QUOTED_IDENTIFIER ON;

IF EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('dbo.school') AND name = 'user_id' AND is_nullable = 0
)
    ALTER TABLE dbo.school ALTER COLUMN user_id UNIQUEIDENTIFIER NULL;

IF EXISTS (
    SELECT 1 FROM sys.key_constraints
     WHERE name = 'uk_school_user' AND parent_object_id = OBJECT_ID('dbo.school')
)
BEGIN
    ALTER TABLE dbo.school DROP CONSTRAINT uk_school_user;
    CREATE UNIQUE INDEX uk_school_user ON dbo.school(user_id) WHERE user_id IS NOT NULL;
END

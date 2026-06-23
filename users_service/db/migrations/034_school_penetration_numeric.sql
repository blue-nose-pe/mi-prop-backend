-- ====================================================================
-- Cliente (C2) — "Penetración: cambiar de Alta/Media/Baja a numérico 1-100,
-- tipeable" (como producción).
--
-- Eliminamos el CHECK ck_school_penetration (migration 023) que solo permitía
-- los strings 'Alta'/'Media'/'Baja'. Ahora la penetración es un número 1-100
-- guardado como string en la misma columna NVARCHAR(10). La validación de
-- rango 1-100 la hace el back (school_handler.isValidPenetration), que además
-- sigue aceptando los valores legacy Alta/Media/Baja para no romper colegios
-- viejos. La columna NO cambia de tipo (NVARCHAR sirve para "75" y "Alta").
--
-- Idempotente — IF EXISTS sobre el constraint. Re-ejecutable.
-- ====================================================================

IF EXISTS (
    SELECT 1 FROM sys.check_constraints
     WHERE name = 'ck_school_penetration' AND parent_object_id = OBJECT_ID('dbo.school')
)
    ALTER TABLE dbo.school DROP CONSTRAINT ck_school_penetration;

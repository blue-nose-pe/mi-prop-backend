-- ====================================================================
-- Cliente (2026-06-04) — el colegio debe tener correo y teléfono de
-- contacto. Son datos importantes que antes no existían en el modelo de
-- colegios (la card del front mostraba "Sin registrar" porque la columna
-- no existía).
--
-- email/phone son OPCIONALES al crear el colegio y se piden al editar
-- (el front valida eso). En BD ambos son NULLable.
--
-- Idempotente — guarda con IF NOT EXISTS sobre sys.columns. Re-ejecutable.
-- ====================================================================

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('dbo.school') AND name = 'email'
)
    ALTER TABLE dbo.school ADD email NVARCHAR(120) NULL;

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('dbo.school') AND name = 'phone'
)
    ALTER TABLE dbo.school ADD phone NVARCHAR(30) NULL;

-- ====================================================================
-- Cliente (2026-06-04) — al editar el colegio se debe poder agregar RUC
-- y poblacion. Estos campos existian en el modelo v1, se quitaron en v2 y
-- el cliente los vuelve a pedir.
--
-- ruc:       RUC del colegio (11 digitos en Peru). NVARCHAR para tolerar
--            formato/guiones. NULLable.
-- poblacion: poblacion estudiantil del colegio (cantidad de alumnos) o la
--            localidad. NVARCHAR libre. NULLable.
--
-- Ambos OPCIONALES en BD; el front los pide al EDITAR el colegio.
-- Idempotente — IF NOT EXISTS sobre sys.columns. Re-ejecutable.
-- ====================================================================

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('dbo.school') AND name = 'ruc'
)
    ALTER TABLE dbo.school ADD ruc NVARCHAR(20) NULL;

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('dbo.school') AND name = 'poblacion'
)
    ALTER TABLE dbo.school ADD poblacion NVARCHAR(60) NULL;

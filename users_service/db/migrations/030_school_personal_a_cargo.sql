-- ====================================================================
-- Cliente (doc observaciones) — "Campos que faltan de la vista original:
-- Personal a cargo, poblacion, penetracion". poblacion (029) y penetracion
-- (023) ya existen; falta "Personal a cargo".
--
-- personal_a_cargo: persona/contacto responsable del programa en el colegio
--                   (texto libre, p.ej. nombre del coordinador interno del
--                   colegio). NULLable. Opcional al crear, editable luego.
--
-- Idempotente — IF NOT EXISTS sobre sys.columns. Re-ejecutable.
-- ====================================================================

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('dbo.school') AND name = 'personal_a_cargo'
)
    ALTER TABLE dbo.school ADD personal_a_cargo NVARCHAR(120) NULL;

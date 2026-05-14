-- Telefono del usuario, opcional. Se sincroniza a HubSpot via UpsertContact
-- (campo `phone` del CRM). Lo guardamos local para evitar round-trip al CRM
-- cada vez que el admin lista users.
--
-- Idempotente: IF NOT EXISTS para que entornos donde el ALTER se aplicó
-- manualmente antes (sin registro en __schema_migrations) no fallen al
-- re-correr el migrator.
IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('users') AND name = 'phone'
)
    ALTER TABLE users ADD phone NVARCHAR(50) NULL;

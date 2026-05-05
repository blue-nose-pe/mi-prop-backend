-- Agrega users.hubspot_record_id para guardar el id que HubSpot devuelve
-- al crear un contact / custom object. Sin esto, los updates posteriores
-- requerirían search-by-email, que es frágil.
--
-- Nota SQL Server: ALTER TABLE ADD COLUMN seguido de CREATE INDEX sobre la
-- nueva columna en el mismo batch falla porque el query compiler valida la
-- columna antes de aplicar el ALTER. Envolvemos los CREATE INDEX en
-- sp_executesql para diferir la compilación al runtime.

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('users') AND name = 'hubspot_record_id'
)
BEGIN
    ALTER TABLE users
        ADD hubspot_record_id NVARCHAR(64) NULL;
END;

IF NOT EXISTS (
    SELECT 1 FROM sys.indexes
     WHERE name = 'idx_users_hubspot_record_id' AND object_id = OBJECT_ID('users')
)
    EXEC sp_executesql N'CREATE INDEX idx_users_hubspot_record_id ON users (hubspot_record_id) WHERE hubspot_record_id IS NOT NULL';

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('school') AND name = 'hubspot_record_id'
)
BEGIN
    ALTER TABLE school
        ADD hubspot_record_id NVARCHAR(64) NULL;
END;

IF NOT EXISTS (
    SELECT 1 FROM sys.indexes
     WHERE name = 'idx_school_hubspot_record_id' AND object_id = OBJECT_ID('school')
)
    EXEC sp_executesql N'CREATE INDEX idx_school_hubspot_record_id ON school (hubspot_record_id) WHERE hubspot_record_id IS NOT NULL';

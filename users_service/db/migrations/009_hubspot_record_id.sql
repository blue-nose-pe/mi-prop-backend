-- Fix de los 3 TODOs del P1 (createEstudiante.js, createAsesor.js, createColegio.js):
-- guardar el record_id que HubSpot devuelve al crear un contact / custom object.
-- Sin esto, los updates posteriores requerirían search-by-email (frágil).

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
    CREATE INDEX idx_users_hubspot_record_id ON users (hubspot_record_id) WHERE hubspot_record_id IS NOT NULL;

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
    CREATE INDEX idx_school_hubspot_record_id ON school (hubspot_record_id) WHERE hubspot_record_id IS NOT NULL;

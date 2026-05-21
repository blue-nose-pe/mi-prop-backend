-- Tabla: school — agrega `int_id` (INT IDENTITY) para integrar con HubSpot.
--
-- Motivacion: la prop custom `mi_proposito___id_colegio` del portal HubSpot
-- de UCSP esta tipada como INTEGER (heredado de Mi Proposito 1.0, que usaba
-- IDs autoincrementales de MySQL). En P2 trabajamos con UUIDs, asi que al
-- mandar el UUID del colegio a HubSpot devuelve 400 INVALID_INTEGER y
-- cancela toda la upsert del contacto del estudiante. Esto rompia el flujo
-- register-with-key: el contacto quedaba sin firstname/lastname/etc.
--
-- Solucion: agregamos una columna `int_id` con IDENTITY(1,1) a la tabla
-- school. SQL Server auto-numera las filas existentes y nuevas. El cliente
-- gRPC users_service expone el int al hubspot_service via SendOTPRequest.
-- Asi mantenemos el UUID como PK semantico interno y exponemos un int chico
-- solo para integrar con sistemas que lo requieran (HubSpot, ETL, etc.).
--
-- ROLLBACK: ALTER TABLE school DROP COLUMN int_id;

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('school') AND name = 'int_id'
)
    ALTER TABLE school ADD int_id INT IDENTITY(1,1) NOT NULL;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_school_int_id' AND object_id = OBJECT_ID('school'))
    EXEC sp_executesql N'CREATE UNIQUE INDEX idx_school_int_id ON school (int_id)';

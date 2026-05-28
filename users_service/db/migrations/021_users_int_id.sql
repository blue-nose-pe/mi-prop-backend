-- Tabla: users — agrega `int_id` (INT IDENTITY) para integrar con HubSpot.
--
-- Motivacion: la prop custom `mi_proposito_asesor_id` del custom object
-- Asesor (2-32448565) en el portal HubSpot de UCSP esta tipada como INTEGER
-- (heredado de Mi Proposito 1.0, que usaba IDs autoincrementales de MySQL).
-- En P2 trabajamos con UUIDs, asi que mandar el UUID al sincronizar una
-- KEY a HubSpot causa que la asociacion Key->Asesor falle (HubSpot devuelve
-- 400 INVALID_INTEGER en el search por mi_proposito_asesor_id).
--
-- Mismo patron que `school.int_id` (migration 020).
--
-- Solucion: agregar columna `int_id` INT IDENTITY(1,1). SQL Server poblara
-- automaticamente filas existentes (ORDER BY default suele ser por
-- primary key insertion, lo cual da numeros estables y monotonos). El
-- proto User expone int_id; el gateway lo resuelve y lo pasa a SyncKey
-- para que se setee en la prop asesor_id del custom object Key.
--
-- ROLLBACK: ALTER TABLE users DROP COLUMN int_id;

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('users') AND name = 'int_id'
)
    ALTER TABLE users ADD int_id INT IDENTITY(1,1) NOT NULL;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_users_int_id' AND object_id = OBJECT_ID('users'))
    EXEC sp_executesql N'CREATE UNIQUE INDEX idx_users_int_id ON users (int_id)';

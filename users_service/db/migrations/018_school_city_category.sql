-- Tabla: school — agrega `city` (texto libre) y `category` (tag UCSP A+/A/B/C/D).
--
-- Motivacion: el front necesita mostrar ciudad y categoria del colegio en
-- la grilla de "Historico de Colegio". El esquema previo solo tenia id,
-- name, active.
--
-- city: texto libre (NVARCHAR(80)). Idealmente sincronizado con HubSpot
-- (propiedad `city` del Custom Object Colegio).
--
-- category: tag interno UCSP. Saber 11 es categoria oficial colombiana, no
-- aplica aca; usamos la convencion compacta A+ / A / B / C / D que es la
-- que el front ya muestra en mockups. Constraint check garantiza que solo
-- esos valores entren a la DB (o NULL si todavia no se clasifico).
--
-- El migrator del proyecto NO soporta el separador GO, asi que los
-- statements que dependen de columnas nuevas se ejecutan via sp_executesql
-- para diferir su compilacion al runtime.

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('school') AND name = 'city'
)
    ALTER TABLE school ADD city NVARCHAR(80) NULL;

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('school') AND name = 'category'
)
    ALTER TABLE school ADD category NVARCHAR(4) NULL;

EXEC sp_executesql N'
IF NOT EXISTS (
    SELECT 1 FROM sys.check_constraints
     WHERE name = ''ck_school_category'' AND parent_object_id = OBJECT_ID(''school'')
)
    ALTER TABLE school WITH NOCHECK ADD CONSTRAINT ck_school_category
        CHECK (category IS NULL OR category IN (N''A+'', N''A'', N''B'', N''C'', N''D''));';

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_school_city' AND object_id = OBJECT_ID('school'))
    EXEC sp_executesql N'CREATE INDEX idx_school_city ON school (city)';

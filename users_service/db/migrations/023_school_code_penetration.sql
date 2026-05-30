-- QUOTED_IDENTIFIER ON es requerido para crear indices filtrados
-- (filtered unique index sobre code WHERE code IS NOT NULL).
SET QUOTED_IDENTIFIER ON;

-- Cliente: agrega `code` (codigo modular MINEDU, obligatorio + unico) y
-- `penetration` (Alta / Media / Baja, editable post-creacion). Tambien
-- amplia la check constraint de `category` (que ahora se renombro a
-- "Segmento" en el FE) para aceptar los 7 valores del nuevo catalogo:
-- A1, A2, B1, B2, C, OP, OR.
--
-- El migrator del proyecto NO soporta GO; statements que dependen de
-- columnas nuevas se ejecutan via sp_executesql para diferir compilacion.

-- 1) code (codigo modular MINEDU)
IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('school') AND name = 'code'
)
    ALTER TABLE school ADD code NVARCHAR(20) NULL;

-- Unique constraint (filtrado para permitir multiples NULL durante el
-- backfill — UCSP llena los codigos modulares progresivamente).
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'ux_school_code' AND object_id = OBJECT_ID('school'))
    EXEC sp_executesql N'CREATE UNIQUE INDEX ux_school_code ON school (code) WHERE code IS NOT NULL';

-- 2) penetration (Alta / Media / Baja)
IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('school') AND name = 'penetration'
)
    ALTER TABLE school ADD penetration NVARCHAR(10) NULL;

EXEC sp_executesql N'
IF NOT EXISTS (
    SELECT 1 FROM sys.check_constraints
     WHERE name = ''ck_school_penetration'' AND parent_object_id = OBJECT_ID(''school'')
)
    ALTER TABLE school WITH NOCHECK ADD CONSTRAINT ck_school_penetration
        CHECK (penetration IS NULL OR penetration IN (N''Alta'', N''Media'', N''Baja''));';

-- 3) Amplia el check de category (Segmento) para aceptar el nuevo
-- catalogo UCSP: A1, A2, B1, B2, C, OP, OR. Tambien preserva los valores
-- legacy A+/A/B/C/D para no romper data existente — UCSP migrara
-- progresivamente con un UPDATE manual.
IF EXISTS (
    SELECT 1 FROM sys.check_constraints
     WHERE name = 'ck_school_category' AND parent_object_id = OBJECT_ID('school')
)
BEGIN
    ALTER TABLE school DROP CONSTRAINT ck_school_category;
END;

EXEC sp_executesql N'
ALTER TABLE school WITH NOCHECK ADD CONSTRAINT ck_school_category
    CHECK (
        category IS NULL OR category IN (
            -- Catalogo nuevo (cliente, 2026)
            N''A1'', N''A2'', N''B1'', N''B2'', N''C'', N''OP'', N''OR'',
            -- Catalogo legacy (preservado durante migracion gradual)
            N''A+'', N''A'', N''B'', N''D''
        )
    );';

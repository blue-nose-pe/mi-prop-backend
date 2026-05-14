-- Tabla: exam — campo `code` (identificador legible que el cliente ve en la UI).
--
-- Reglas:
--   * NVARCHAR(50), unique. La aplicacion autogenera si el cliente no lo manda.
--   * Formato del autogen: {PREFIX}-{YYYY}-V{version} donde PREFIX deriva del
--     exam_type:
--         vocacional -> VOCC
--         simulacro  -> SIME
--         habitos    -> ESTI
--   * Backfill para filas existentes: misma regla pero con sufijo de 4 chars
--     del id, para garantizar unicidad incluso cuando hay multiples clones
--     con la misma (tipo, version) preexistentes.
--
-- Migrator del proyecto NO soporta el separador GO, asi que los statements
-- que dependen de la nueva columna (UPDATE, ALTER COLUMN, CREATE INDEX) se
-- ejecutan via sp_executesql para diferir su compilacion al runtime.

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('exam') AND name = 'code'
)
    ALTER TABLE exam ADD code NVARCHAR(50) NULL;

EXEC sp_executesql N'
UPDATE e
   SET code = CONCAT(
                CASE et.code
                    WHEN ''vocacional'' THEN ''VOCC''
                    WHEN ''simulacro''  THEN ''SIME''
                    WHEN ''habitos''    THEN ''ESTI''
                    ELSE ''EXAM''
                END,
                ''-'',
                FORMAT(e.created_at, ''yyyy''),
                ''-V'', e.version,
                ''-'',
                UPPER(LEFT(CONVERT(NVARCHAR(36), e.id), 4))
              )
  FROM exam e
  JOIN exam_type et ON et.id = e.exam_type_id
 WHERE e.code IS NULL;';

EXEC sp_executesql N'
IF EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID(''exam'') AND name = ''code'' AND is_nullable = 1
)
    ALTER TABLE exam ALTER COLUMN code NVARCHAR(50) NOT NULL;';

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'ux_exam_code' AND object_id = OBJECT_ID('exam'))
    EXEC sp_executesql N'CREATE UNIQUE INDEX ux_exam_code ON exam (code)';

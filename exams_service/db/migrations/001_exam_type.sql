-- Tabla: exam_type (catálogo).
-- Codes para Mi Propósito: vocacional, simulacro, habitos.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'exam_type')
BEGIN
    CREATE TABLE exam_type (
        id          INT            IDENTITY(1,1) NOT NULL PRIMARY KEY,
        code        NVARCHAR(64)   NOT NULL,
        name        NVARCHAR(120)  NOT NULL,
        active      BIT            NOT NULL CONSTRAINT df_exam_type_active DEFAULT 1,
        created_at  DATETIME2(7)   NOT NULL CONSTRAINT df_exam_type_created_at DEFAULT SYSUTCDATETIME(),
        updated_at  DATETIME2(7)   NULL,
        CONSTRAINT uk_exam_type_code UNIQUE (code)
    );
END;

IF OBJECT_ID('trg_exam_type_updated_at', 'TR') IS NOT NULL
    DROP TRIGGER trg_exam_type_updated_at;
EXEC('
CREATE TRIGGER trg_exam_type_updated_at
ON exam_type
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE t SET updated_at = SYSUTCDATETIME()
      FROM exam_type t INNER JOIN inserted i ON t.id = i.id;
END
');

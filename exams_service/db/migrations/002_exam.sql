-- Tabla: exam.
-- Cambios vs MySQL legacy:
--   * id INT → UNIQUEIDENTIFIER (consistencia con users.id de db_users).
--   * school_id INT → UNIQUEIDENTIFIER (cross-DB lógica a db_users.school.id;
--     SIN FK física porque es cross-DB en Azure SQL).
--   * parent_exam_id + version: versionado por linaje. Editar preguntas
--     crea un NUEVO exam con parent_exam_id apuntando al original; los
--     exam_attempt antiguos siguen referenciando al exam original.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'exam')
BEGIN
    CREATE TABLE exam (
        id                UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_exam_id DEFAULT NEWID() PRIMARY KEY,
        exam_type_id      INT              NOT NULL,
        school_id         UNIQUEIDENTIFIER NULL,
        parent_exam_id    UNIQUEIDENTIFIER NULL,
        version           INT              NOT NULL CONSTRAINT df_exam_version DEFAULT 1,
        name              NVARCHAR(120)    NOT NULL,
        start_at          DATETIME2(7)     NOT NULL,
        end_at            DATETIME2(7)     NOT NULL,
        max_participants  INT              NOT NULL CONSTRAINT df_exam_max_part DEFAULT 0,
        published         BIT              NOT NULL CONSTRAINT df_exam_published DEFAULT 0,
        active            BIT              NOT NULL CONSTRAINT df_exam_active DEFAULT 1,
        created_at        DATETIME2(7)     NOT NULL CONSTRAINT df_exam_created_at DEFAULT SYSUTCDATETIME(),
        updated_at        DATETIME2(7)     NULL,
        CONSTRAINT fk_exam_type   FOREIGN KEY (exam_type_id)   REFERENCES exam_type(id),
        CONSTRAINT fk_exam_parent FOREIGN KEY (parent_exam_id) REFERENCES exam(id)
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_exam_type' AND object_id = OBJECT_ID('exam'))
    CREATE INDEX idx_exam_type   ON exam (exam_type_id);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_exam_school' AND object_id = OBJECT_ID('exam'))
    CREATE INDEX idx_exam_school ON exam (school_id);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_exam_dates' AND object_id = OBJECT_ID('exam'))
    CREATE INDEX idx_exam_dates  ON exam (start_at, end_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_exam_parent' AND object_id = OBJECT_ID('exam'))
    CREATE INDEX idx_exam_parent ON exam (parent_exam_id);

IF OBJECT_ID('trg_exam_updated_at', 'TR') IS NOT NULL DROP TRIGGER trg_exam_updated_at;
EXEC('
CREATE TRIGGER trg_exam_updated_at
ON exam
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE e SET updated_at = SYSUTCDATETIME()
      FROM exam e INNER JOIN inserted i ON e.id = i.id;
END
');

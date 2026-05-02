-- Banco de preguntas. Reusables entre exams (asociados via exam_question).

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'question')
BEGIN
    CREATE TABLE question (
        id          UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_question_id DEFAULT NEWID() PRIMARY KEY,
        text        NVARCHAR(MAX)    NOT NULL,
        category    NVARCHAR(64)     NULL,            -- útil para vocacional (categoría: matemática, lengua...)
        active      BIT              NOT NULL CONSTRAINT df_question_active DEFAULT 1,
        created_at  DATETIME2(7)     NOT NULL CONSTRAINT df_question_created_at DEFAULT SYSUTCDATETIME(),
        updated_at  DATETIME2(7)     NULL
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_question_category' AND object_id = OBJECT_ID('question'))
    CREATE INDEX idx_question_category ON question (category);

IF OBJECT_ID('trg_question_updated_at', 'TR') IS NOT NULL DROP TRIGGER trg_question_updated_at;
EXEC('
CREATE TRIGGER trg_question_updated_at
ON question
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE q SET updated_at = SYSUTCDATETIME()
      FROM question q INNER JOIN inserted i ON q.id = i.id;
END
');

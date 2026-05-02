-- Intentos de un user contra un exam.
-- user_id: cross-DB lógico hacia db_users.users.id (UNIQUEIDENTIFIER).

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'exam_attempt')
BEGIN
    CREATE TABLE exam_attempt (
        id            UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_exam_attempt_id DEFAULT NEWID() PRIMARY KEY,
        exam_id       UNIQUEIDENTIFIER NOT NULL,
        user_id       UNIQUEIDENTIFIER NOT NULL,
        key_id        UNIQUEIDENTIFIER NULL,                 -- key validada (cross-DB → keys_service.key.id)
        score         INT              NULL,                  -- calculado al FinishAttempt
        max_score     INT              NULL,
        started_at    DATETIME2(7)     NOT NULL CONSTRAINT df_exam_attempt_started DEFAULT SYSUTCDATETIME(),
        submitted_at  DATETIME2(7)     NULL,
        CONSTRAINT fk_exam_attempt_exam
            FOREIGN KEY (exam_id) REFERENCES exam(id) ON DELETE CASCADE
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_exam_attempt_exam' AND object_id = OBJECT_ID('exam_attempt'))
    CREATE INDEX idx_exam_attempt_exam ON exam_attempt (exam_id);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_exam_attempt_user' AND object_id = OBJECT_ID('exam_attempt'))
    CREATE INDEX idx_exam_attempt_user ON exam_attempt (user_id, submitted_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_exam_attempt_key' AND object_id = OBJECT_ID('exam_attempt'))
    CREATE INDEX idx_exam_attempt_key  ON exam_attempt (key_id);

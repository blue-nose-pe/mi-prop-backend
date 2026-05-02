-- Respuesta de un attempt a una pregunta.
-- PK compuesta (attempt, question) → un attempt elige UNA opción por pregunta.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'attempt_answer')
BEGIN
    CREATE TABLE attempt_answer (
        exam_attempt_id     UNIQUEIDENTIFIER NOT NULL,
        question_id         UNIQUEIDENTIFIER NOT NULL,
        question_option_id  UNIQUEIDENTIFIER NOT NULL,
        answered_at         DATETIME2(7)     NOT NULL CONSTRAINT df_attempt_answer_at DEFAULT SYSUTCDATETIME(),
        CONSTRAINT pk_attempt_answer
            PRIMARY KEY (exam_attempt_id, question_id),
        CONSTRAINT fk_attempt_answer_attempt
            FOREIGN KEY (exam_attempt_id)    REFERENCES exam_attempt(id)    ON DELETE CASCADE,
        CONSTRAINT fk_attempt_answer_q
            FOREIGN KEY (question_id)        REFERENCES question(id),
        CONSTRAINT fk_attempt_answer_option
            FOREIGN KEY (question_option_id) REFERENCES question_option(id)
    );
END;

-- Opciones de respuesta (multiple choice).

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'question_option')
BEGIN
    CREATE TABLE question_option (
        id           UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_question_option_id DEFAULT NEWID() PRIMARY KEY,
        question_id  UNIQUEIDENTIFIER NOT NULL,
        text         NVARCHAR(MAX)    NOT NULL,
        is_correct   BIT              NOT NULL CONSTRAINT df_question_option_correct DEFAULT 0,
        sort_order   INT              NOT NULL CONSTRAINT df_question_option_sort DEFAULT 0,
        CONSTRAINT fk_question_option FOREIGN KEY (question_id)
            REFERENCES question(id) ON DELETE CASCADE
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_question_option_q' AND object_id = OBJECT_ID('question_option'))
    CREATE INDEX idx_question_option_q ON question_option (question_id, sort_order);

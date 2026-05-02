-- Una respuesta = una entrada del survey por un user.
-- exam_attempt_id es opcional: cuando trigger_kind=post_test, ata la
-- encuesta al attempt que disparó la invitación.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'survey_response')
BEGIN
    CREATE TABLE survey_response (
        id              UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_survey_response_id DEFAULT NEWID() PRIMARY KEY,
        survey_id       UNIQUEIDENTIFIER NOT NULL,
        user_id         UNIQUEIDENTIFIER NOT NULL,
        exam_attempt_id UNIQUEIDENTIFIER NULL,
        submitted_at    DATETIME2(7)     NOT NULL CONSTRAINT df_survey_response_at DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_survey_response_survey
            FOREIGN KEY (survey_id) REFERENCES survey(id) ON DELETE CASCADE
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_survey_response_user' AND object_id = OBJECT_ID('survey_response'))
    CREATE INDEX idx_survey_response_user   ON survey_response (user_id, submitted_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_survey_response_survey' AND object_id = OBJECT_ID('survey_response'))
    CREATE INDEX idx_survey_response_survey ON survey_response (survey_id, submitted_at);

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'survey_answer')
BEGIN
    CREATE TABLE survey_answer (
        response_id  UNIQUEIDENTIFIER NOT NULL,
        question_id  UNIQUEIDENTIFIER NOT NULL,
        value_text   NVARCHAR(MAX)    NULL,
        value_number INT              NULL,
        CONSTRAINT pk_survey_answer
            PRIMARY KEY (response_id, question_id),
        CONSTRAINT fk_survey_answer_response
            FOREIGN KEY (response_id) REFERENCES survey_response(id) ON DELETE CASCADE,
        CONSTRAINT fk_survey_answer_question
            FOREIGN KEY (question_id) REFERENCES survey_question(id)
    );
END;

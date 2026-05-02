-- Pregunta de encuesta. kind define el tipo:
--   * scale  → 1-5 / 1-10 (escala numérica con label opcional).
--   * nps    → 0-10 detractor/passive/promoter.
--   * single → multiple-choice (1 opción de N).
--   * multi  → multiple-choice (M de N).
--   * open   → texto libre.
-- options_json: para single/multi/scale guarda el catálogo de opciones
-- como JSON para no normalizar de más (las encuestas son raras).

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'survey_question')
BEGIN
    CREATE TABLE survey_question (
        id           UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_survey_question_id DEFAULT NEWID() PRIMARY KEY,
        survey_id    UNIQUEIDENTIFIER NOT NULL,
        text         NVARCHAR(MAX)    NOT NULL,
        kind         NVARCHAR(16)     NOT NULL,
        sort_order   INT              NOT NULL CONSTRAINT df_survey_question_sort DEFAULT 0,
        options_json NVARCHAR(MAX)    NULL,
        required     BIT              NOT NULL CONSTRAINT df_survey_question_required DEFAULT 1,
        CONSTRAINT fk_survey_question
            FOREIGN KEY (survey_id) REFERENCES survey(id) ON DELETE CASCADE,
        CONSTRAINT ck_survey_question_kind
            CHECK (kind IN ('scale','nps','single','multi','open'))
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_survey_question_survey' AND object_id = OBJECT_ID('survey_question'))
    CREATE INDEX idx_survey_question_survey ON survey_question (survey_id, sort_order);

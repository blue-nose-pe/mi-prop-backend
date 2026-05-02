-- Asociación N:M exam ↔ question, con puntaje y orden.
-- Modificar las preguntas de un exam = clonarlo (parent_exam_id) y
-- recrear estas asociaciones — los attempts antiguos siguen apuntando
-- al exam original con sus exam_question originales.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'exam_question')
BEGIN
    CREATE TABLE exam_question (
        exam_id      UNIQUEIDENTIFIER NOT NULL,
        question_id  UNIQUEIDENTIFIER NOT NULL,
        points       INT              NOT NULL CONSTRAINT df_exam_question_points DEFAULT 1,
        sort_order   INT              NOT NULL CONSTRAINT df_exam_question_sort DEFAULT 0,
        CONSTRAINT pk_exam_question
            PRIMARY KEY (exam_id, question_id),
        CONSTRAINT fk_exam_question_exam
            FOREIGN KEY (exam_id)     REFERENCES exam(id)     ON DELETE CASCADE,
        CONSTRAINT fk_exam_question_q
            FOREIGN KEY (question_id) REFERENCES question(id)
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_exam_question_order' AND object_id = OBJECT_ID('exam_question'))
    CREATE INDEX idx_exam_question_order ON exam_question (exam_id, sort_order);

-- Finaliza la migracion 013_question_kind_full que quedo parcialmente
-- aplicada: el kind constraint y la nullable de question_option_id si se
-- aplicaron, pero el PK compuesto y la columna answer_text no.
--
-- Cambios:
--   1) option_id_pk computed column PERSISTED (sentinel zero-GUID cuando
--      question_option_id es NULL). Necesario para que T-SQL acepte un
--      PK que incluya una columna nullable.
--   2) PK pk_attempt_answer sobre (exam_attempt_id, question_id, option_id_pk).
--      Permite multi-fila por (attempt, question) para MULTIPLE_CHOICE.
--   3) answer_text NVARCHAR(MAX) NULL para OPEN_TEXT.

SET QUOTED_IDENTIFIER ON;

-- 1) option_id_pk si no existe.
IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('dbo.attempt_answer') AND name = 'option_id_pk'
)
BEGIN
    ALTER TABLE dbo.attempt_answer
      ADD option_id_pk AS (
        ISNULL(question_option_id, CONVERT(UNIQUEIDENTIFIER, '00000000-0000-0000-0000-000000000000'))
      ) PERSISTED NOT NULL;
END;

-- 2) PK compuesta si no existe.
IF NOT EXISTS (
    SELECT 1 FROM sys.key_constraints
     WHERE name = 'pk_attempt_answer'
       AND parent_object_id = OBJECT_ID('dbo.attempt_answer')
)
BEGIN
    ALTER TABLE dbo.attempt_answer
      ADD CONSTRAINT pk_attempt_answer
          PRIMARY KEY (exam_attempt_id, question_id, option_id_pk);
END;

-- 3) answer_text.
IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('dbo.attempt_answer') AND name = 'answer_text'
)
BEGIN
    ALTER TABLE dbo.attempt_answer ADD answer_text NVARCHAR(MAX) NULL;
END;

-- QUOTED_IDENTIFIER debe estar ON para crear computed columns PERSISTED
-- (T-SQL strict mode requirement). Sin esto el ADD option_id_pk falla.
SET QUOTED_IDENTIFIER ON;

-- Extiende `question.kind` y reshapea `attempt_answer` para soportar
-- MULTIPLE_CHOICE (varias opciones por pregunta) y OPEN_TEXT (respuesta
-- como texto libre sin option_id).
--
-- Cambios:
--   1) question.kind CHECK ahora acepta MULTIPLE_CHOICE y OPEN_TEXT.
--   2) attempt_answer.question_option_id pasa a NULLABLE (OPEN_TEXT lo
--      deja en NULL).
--   3) PK compuesta de attempt_answer se reescribe para incluir option_id,
--      asi un attempt puede tener N filas por question_id (una por
--      opcion seleccionada en MULTIPLE_CHOICE) sin romper la unicidad.
--      Para OPEN_TEXT donde option_id es NULL, usamos un GUID sintetico
--      `00000000-0000-0000-0000-000000000000` como sentinel en la PK
--      (T-SQL NO permite columnas NULL en PK), pero la columna real
--      sigue NULL para conservar la semantica.
--   4) Nueva columna answer_text NVARCHAR(MAX) NULL para OPEN_TEXT.
--
-- MULTIPLE_CHOICE scoring (manejado en codigo, no en SQL): all-correct
-- selected and no incorrect → full points; sino 0. OPEN_TEXT = 0 puntos
-- automatico (corrige el especialista manualmente).

-- 1) Actualiza CHECK constraint de question.kind
IF EXISTS (
    SELECT 1 FROM sys.check_constraints WHERE name = 'ck_question_kind'
)
BEGIN
    ALTER TABLE dbo.question DROP CONSTRAINT ck_question_kind;
END;

EXEC('ALTER TABLE dbo.question
        ADD CONSTRAINT ck_question_kind
            CHECK (kind IN (''SINGLE_CHOICE'',''TRUE_FALSE'',''SCALE'',''MULTIPLE_CHOICE'',''OPEN_TEXT''))');

-- 2) Re-shape attempt_answer
-- 2a) Drop PK actual (attempt, question) — bloquea el cambio a multi-row.
IF EXISTS (
    SELECT 1 FROM sys.key_constraints
     WHERE name = 'pk_attempt_answer' AND parent_object_id = OBJECT_ID('dbo.attempt_answer')
)
BEGIN
    ALTER TABLE dbo.attempt_answer DROP CONSTRAINT pk_attempt_answer;
END;

-- 2b) question_option_id pasa a NULLABLE.
-- T-SQL no permite ALTER COLUMN si la columna esta referenciada por una
-- FK; conservamos la FK al final, asi que primero la dropeamos.
IF EXISTS (
    SELECT 1 FROM sys.foreign_keys
     WHERE name = 'fk_attempt_answer_option' AND parent_object_id = OBJECT_ID('dbo.attempt_answer')
)
BEGIN
    ALTER TABLE dbo.attempt_answer DROP CONSTRAINT fk_attempt_answer_option;
END;

ALTER TABLE dbo.attempt_answer ALTER COLUMN question_option_id UNIQUEIDENTIFIER NULL;

-- Re-agrega FK con NULL permitido (FK acepta NULL automaticamente).
ALTER TABLE dbo.attempt_answer
  ADD CONSTRAINT fk_attempt_answer_option
      FOREIGN KEY (question_option_id) REFERENCES dbo.question_option(id);

-- 2c) Nueva PK: (attempt, question, option_id_sentinel). T-SQL exige NOT NULL
-- en PK, asi que creamos una columna calculada PERSISTED con el GUID
-- sentinel cuando option_id es NULL. (Alternativa: PK no clustered + unique
-- filtered index; pero la columna calculada es mas directa para los repos.)
ALTER TABLE dbo.attempt_answer
  ADD option_id_pk AS (ISNULL(question_option_id, CONVERT(UNIQUEIDENTIFIER, '00000000-0000-0000-0000-000000000000'))) PERSISTED NOT NULL;

ALTER TABLE dbo.attempt_answer
  ADD CONSTRAINT pk_attempt_answer
      PRIMARY KEY (exam_attempt_id, question_id, option_id_pk);

-- 3) answer_text para OPEN_TEXT.
IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('dbo.attempt_answer') AND name = 'answer_text'
)
BEGIN
    ALTER TABLE dbo.attempt_answer ADD answer_text NVARCHAR(MAX) NULL;
END;

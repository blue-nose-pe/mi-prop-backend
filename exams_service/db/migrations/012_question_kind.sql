-- Agrega `kind` al banco de preguntas. Permite que el UI renderice
-- correctamente cada tipo y que reporting sepa interpretar el option_id
-- elegido (p. ej. SCALE: sort_order = peso Likert, no es_correcta).
--
-- Valores soportados (string para evitar mantener tabla de catalogo):
--   SINGLE_CHOICE  - default. N opciones, 1 is_correct. Score binario.
--   TRUE_FALSE     - 2 opciones (V/F). Score binario (igual modelo).
--   SCALE          - opciones "1".."5" con sort_order=peso. Sin is_correct
--                    => score=0 a nivel exam, pero el sort_order del option
--                    elegido es usable como Likert por analytics.
--
-- MULTIPLE_CHOICE y OPEN_TEXT no se modelan aqui: requeririan cambiar el
-- PK de attempt_answer (multi-fila por pregunta) y/o agregar columna
-- answer_text. Quedan como futuro trabajo.

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('dbo.question') AND name = 'kind'
)
BEGIN
    EXEC('ALTER TABLE dbo.question
            ADD kind NVARCHAR(32) NOT NULL
                CONSTRAINT df_question_kind DEFAULT ''SINGLE_CHOICE''');
END;

-- Check constraint defensivo: previene typos desde el back. EXEC para que
-- el batch se compile DESPUES del ALTER previo (el batch unico no
-- conoceria la columna `kind` que el IF de arriba acaba de crear).
IF NOT EXISTS (
    SELECT 1 FROM sys.check_constraints
     WHERE name = 'ck_question_kind'
)
BEGIN
    EXEC('ALTER TABLE dbo.question
            ADD CONSTRAINT ck_question_kind
                CHECK (kind IN (''SINGLE_CHOICE'',''TRUE_FALSE'',''SCALE''))');
END;

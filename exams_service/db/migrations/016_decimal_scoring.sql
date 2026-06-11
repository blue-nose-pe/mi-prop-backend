-- Puntaje FLOAT para fidelidad con producción.
--
-- El Examen Nacional usa 1.25 puntos por respuesta correcta (80 preguntas =
-- 100 puntos exactos). Con points como INT el 1.25 se truncaba a 1 y el
-- examen quedaba en /80 en vez de /100. Pasamos los campos de puntaje a FLOAT
-- (1.25/100/450 son exactos en float64). El simulacro UCSP (5 / +1 blanco)
-- sigue igual (5.0 / 1.0).
--
-- Las columnas tienen default constraints (DEFAULT 0) que hay que dropear
-- antes del ALTER COLUMN y volver a crear después.
SET QUOTED_IDENTIFIER ON;
SET ANSI_NULLS ON;

DECLARE @sql NVARCHAR(MAX) = N'';
SELECT @sql = @sql + 'ALTER TABLE ' + QUOTENAME(OBJECT_NAME(dc.parent_object_id)) + ' DROP CONSTRAINT ' + QUOTENAME(dc.name) + ';'
FROM sys.default_constraints dc
JOIN sys.columns c ON c.object_id = dc.parent_object_id AND c.column_id = dc.parent_column_id
WHERE (OBJECT_NAME(dc.parent_object_id) = 'exam_question' AND c.name IN ('points', 'points_incorrect', 'points_blank'))
   OR (OBJECT_NAME(dc.parent_object_id) = 'exam_attempt' AND c.name IN ('score', 'max_score'));
IF LEN(@sql) > 0 EXEC sp_executesql @sql;

ALTER TABLE exam_question ALTER COLUMN points FLOAT NOT NULL;
ALTER TABLE exam_question ALTER COLUMN points_incorrect FLOAT NOT NULL;
ALTER TABLE exam_question ALTER COLUMN points_blank FLOAT NOT NULL;
ALTER TABLE exam_attempt ALTER COLUMN score FLOAT NULL;
ALTER TABLE exam_attempt ALTER COLUMN max_score FLOAT NULL;

IF NOT EXISTS (SELECT 1 FROM sys.default_constraints WHERE name = 'df_exam_question_points')
    ALTER TABLE exam_question ADD CONSTRAINT df_exam_question_points DEFAULT 0 FOR points;
IF NOT EXISTS (SELECT 1 FROM sys.default_constraints WHERE name = 'df_exam_question_points_incorrect')
    ALTER TABLE exam_question ADD CONSTRAINT df_exam_question_points_incorrect DEFAULT 0 FOR points_incorrect;
IF NOT EXISTS (SELECT 1 FROM sys.default_constraints WHERE name = 'df_exam_question_points_blank')
    ALTER TABLE exam_question ADD CONSTRAINT df_exam_question_points_blank DEFAULT 0 FOR points_blank;

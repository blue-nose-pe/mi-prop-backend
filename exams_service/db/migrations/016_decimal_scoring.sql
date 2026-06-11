-- Puntaje DECIMAL para fidelidad con producción.
--
-- El Examen Nacional usa 1.25 puntos por respuesta correcta (80 preguntas =
-- 100 puntos exactos). Con points como INT, el 1.25 se truncaba a 1 y el
-- examen quedaba en /80 en vez de /100. Pasamos los campos de puntaje a
-- FLOAT para representar 1.25 y cualquier esquema de prod fielmente.
-- El simulacro UCSP (5 / +1 blanco) sigue igual (5.00 / 1.00).

-- Puntaje por pregunta del examen.
ALTER TABLE exam_question ALTER COLUMN points FLOAT NOT NULL;
ALTER TABLE exam_question ALTER COLUMN points_incorrect FLOAT NOT NULL;
ALTER TABLE exam_question ALTER COLUMN points_blank FLOAT NOT NULL;

-- Puntaje calculado del intento.
ALTER TABLE exam_attempt ALTER COLUMN score FLOAT NULL;
ALTER TABLE exam_attempt ALTER COLUMN max_score FLOAT NULL;

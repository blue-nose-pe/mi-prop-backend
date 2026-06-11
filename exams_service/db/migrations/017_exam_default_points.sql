-- Reglas de puntaje por DEFECTO a nivel examen.
--
-- El builder del admin define al crear el examen cuánto vale una respuesta
-- correcta / incorrecta / en blanco. Estos defaults se copian a cada
-- exam_question al agregarla (el scoring real sigue siendo por pregunta,
-- lo que permite puntajes individuales si el admin lo elige).
-- Fieles a prod: UCSP 5/0/1 · Nacional 1.25/0/0.
IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('exam') AND name = 'default_points')
    ALTER TABLE exam ADD default_points FLOAT NOT NULL CONSTRAINT df_exam_default_points DEFAULT 1;
IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('exam') AND name = 'default_points_incorrect')
    ALTER TABLE exam ADD default_points_incorrect FLOAT NOT NULL CONSTRAINT df_exam_default_points_incorrect DEFAULT 0;
IF NOT EXISTS (SELECT 1 FROM sys.columns WHERE object_id = OBJECT_ID('exam') AND name = 'default_points_blank')
    ALTER TABLE exam ADD default_points_blank FLOAT NOT NULL CONSTRAINT df_exam_default_points_blank DEFAULT 0;

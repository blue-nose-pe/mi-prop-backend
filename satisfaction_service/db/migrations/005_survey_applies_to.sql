-- Cliente: el survey-builder reemplazó el dropdown "trigger" (timing:
-- post_test | on_demand | recurring) por "Aplicar a" — a qué tipo de examen
-- aplica la encuesta (simulacro | vocacional | estilos). Reutilizamos la
-- columna trigger_kind para llevar ese valor: cuando el alumno termina un
-- examen, el front busca la encuesta publicada cuyo trigger_kind == tipo de
-- examen y se la ofrece.
--
-- La CHECK original (migration 001) sólo permitía los 3 valores de timing,
-- por lo que guardar "simulacro" desde el builder fallaba con constraint
-- violation (PATCH /api/surveys/{id} → 500). Relajamos la CHECK a la unión:
-- back-compat con surveys viejos (post_test/on_demand/recurring) + los
-- nuevos exam-types. Un batch, sin GO (el migrator no lo soporta).

IF EXISTS (SELECT 1 FROM sys.check_constraints WHERE name = 'ck_survey_trigger')
    ALTER TABLE survey DROP CONSTRAINT ck_survey_trigger;

ALTER TABLE survey ADD CONSTRAINT ck_survey_trigger
    CHECK (trigger_kind IN (
        'post_test', 'on_demand', 'recurring',
        'simulacro', 'vocacional', 'estilos'
    ));

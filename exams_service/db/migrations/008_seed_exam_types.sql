-- Seeds idempotentes: los 3 módulos de Mi Propósito.

MERGE INTO exam_type AS target
USING (VALUES
    ('vocacional', 'Test Vocacional'),
    ('simulacro',  'Simulacro de Admisión'),
    ('habitos',    'Estilos de Aprendizaje / Hábitos')
) AS source (code, name)
ON target.code = source.code
WHEN MATCHED THEN
    UPDATE SET name = source.name
WHEN NOT MATCHED THEN
    INSERT (code, name)
    VALUES (source.code, source.name);

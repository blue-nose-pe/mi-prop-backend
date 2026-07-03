-- Reconcilia el contador `current_uses` de las llaves a la REALIDAD:
-- = cantidad de alumnos DISTINTOS que rindieron un examen con esa llave.
-- El seed lo dejaba inflado (ej. 43/50 sin exámenes reales), lo que confundía
-- en el portal del colegio, el asistente y la reportería.
--
-- Ejecutar contra el MSSQL del cluster:
--   kubectl -n miproposito exec -i mssql-server-0 -- /opt/mssql-tools18/bin/sqlcmd \
--     -S localhost -U SA -P "$MSSQL_SA_PASSWORD" -C -W -i /dev/stdin < reconcile_usos.sql
--
-- La tabla [key] exige SET QUOTED_IDENTIFIER ON para el UPDATE (por sus índices).
SET NOCOUNT ON;
SET QUOTED_IDENTIFIER ON;

UPDATE k
SET current_uses = x.real
FROM db_keys.dbo.[key] k
CROSS APPLY (
  SELECT COUNT(DISTINCT a.user_id) AS real
  FROM db_exams.dbo.exam_attempt a
  WHERE a.key_id = k.id
) x
WHERE k.current_uses <> x.real;

SELECT @@ROWCOUNT AS keys_actualizadas, (SELECT SUM(current_uses) FROM db_keys.dbo.[key]) AS suma_usos;

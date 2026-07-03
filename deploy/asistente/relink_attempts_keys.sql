-- Re-vincula intentos de examen HUÉRFANOS (sin key_id) a una llave real de su
-- colegio y tipo, y reconcilia current_uses. Corrige el artefacto del seed en
-- que los intentos existían pero no apuntaban a ninguna llave, produciendo la
-- incoherencia "el colegio tiene 97 rendidos pero sus llaves muestran 0".
--
-- En PRODUCCIÓN esto no pasa (todo examen se rinde con una llave); es solo para
-- datos de prueba (seed). Aplicado 2026-07-03: 551 huérfanos → 545 vinculados,
-- 6 quedan sin llave de su tipo (no se inventan llaves), 133 llaves reconciliadas.
--
-- Idempotente: solo toca attempts submitted con key_id NULL. Reversible con la
-- tabla de backup db_exams.dbo.exam_attempt_keyid_bak_YYYYMMDD.
--
-- Ejecutar:  sqlcmd -S localhost -U SA -P "$MSSQL_SA_PASSWORD" -C -i relink_attempts_keys.sql

SET NOCOUNT ON;
SET QUOTED_IDENTIFIER ON;

-- 0) Backup del mapeo actual (attempt -> key_id) de TODOS los attempts.
IF OBJECT_ID('db_exams.dbo.exam_attempt_keyid_bak_20260703') IS NOT NULL
  DROP TABLE db_exams.dbo.exam_attempt_keyid_bak_20260703;
SELECT id AS attempt_id, key_id AS old_key_id
INTO db_exams.dbo.exam_attempt_keyid_bak_20260703
FROM db_exams.dbo.exam_attempt;

BEGIN TRAN;

-- 1) Asignación: cada alumno con intentos huérfanos de un tipo va a UNA llave
--    activa de su colegio+tipo. Spillover por aforo (max_uses) cuando un tipo
--    tiene más alumnos que el aforo de la primera llave.
;WITH orph AS (
  SELECT a.id AS attempt_id, u.school_id, e.exam_type_id, a.user_id
  FROM db_exams.dbo.exam_attempt a
  JOIN db_users.dbo.users u ON u.id = a.user_id
  JOIN db_exams.dbo.exam  e ON e.id = a.exam_id
  WHERE a.submitted_at IS NOT NULL AND a.key_id IS NULL
),
ur AS (  -- alumnos distintos por (colegio,tipo), con rango estable
  SELECT school_id, exam_type_id, user_id,
         ROW_NUMBER() OVER (PARTITION BY school_id, exam_type_id ORDER BY user_id) AS urank
  FROM (SELECT DISTINCT school_id, exam_type_id, user_id FROM orph) d
),
keys_ord AS (  -- llaves activas por (colegio,tipo) con ventana de capacidad acumulada
  SELECT id AS key_id, school_id, exam_type_id,
         SUM(max_uses) OVER (PARTITION BY school_id, exam_type_id ORDER BY created_at, code
             ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS cum_cap,
         SUM(max_uses) OVER (PARTITION BY school_id, exam_type_id ORDER BY created_at, code
             ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) - max_uses AS prev_cap
  FROM db_keys.dbo.[key] WHERE active = 1
),
user_key AS (  -- cada alumno cae en la llave cuya ventana contiene su rango
  SELECT ur.school_id, ur.exam_type_id, ur.user_id, k.key_id
  FROM ur JOIN keys_ord k
    ON k.school_id = ur.school_id AND k.exam_type_id = ur.exam_type_id
   AND ur.urank > k.prev_cap AND ur.urank <= k.cum_cap
)
UPDATE a
SET a.key_id = uk.key_id
FROM db_exams.dbo.exam_attempt a
JOIN db_users.dbo.users u ON u.id = a.user_id
JOIN db_exams.dbo.exam  e ON e.id = a.exam_id
JOIN user_key uk ON uk.user_id = a.user_id AND uk.school_id = u.school_id AND uk.exam_type_id = e.exam_type_id
WHERE a.submitted_at IS NOT NULL AND a.key_id IS NULL;

-- 2) current_uses = alumnos DISTINTOS con examen realmente rendido por llave.
UPDATE k
SET k.current_uses = ISNULL(x.cnt, 0)
FROM db_keys.dbo.[key] k
OUTER APPLY (
  SELECT COUNT(DISTINCT a.user_id) AS cnt
  FROM db_exams.dbo.exam_attempt a
  WHERE a.key_id = k.id AND a.submitted_at IS NOT NULL
) x;

COMMIT;

-- Verificación rápida
SELECT 'orphan_restantes' AS t, COUNT(*) AS n
FROM db_exams.dbo.exam_attempt WHERE submitted_at IS NOT NULL AND key_id IS NULL;

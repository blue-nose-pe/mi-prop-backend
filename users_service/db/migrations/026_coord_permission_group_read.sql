-- ====================================================================
-- Cliente (2026-06-01) — fix: el COORDINADOR no podía crear estudiantes.
--
-- La migration 025 le dio `db_users.users.write` (ve el form Crear
-- Estudiante), pero el form (create-form.component) resuelve el grupo
-- `student_permissions` llamando a GET /api/permission-groups para asignar
-- el permission_group_id del nuevo estudiante. Ese endpoint exige
-- `db_users.permission_group.read`, que coordinador_permissions NO tenía →
-- 403 → studentGroupId queda null → createStudent() aborta con un toast y
-- NUNCA postea. Resultado: el coordinador ve el form pero no puede enviar.
--
-- Agregamos db_users.permission_group.read (solo lectura del catálogo de
-- grupos) a coordinador_permissions para completar la capacidad de gestión
-- de estudiantes que la 025 pretendía habilitar.
--
-- Idempotente — NOT EXISTS. Re-ejecutable sin efectos colaterales.
-- ====================================================================

SET QUOTED_IDENTIFIER ON;

INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'coordinador_permissions'
   AND p.code = 'db_users.permission_group.read'
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission existing
      WHERE existing.permission_group_id = g.id
        AND existing.permission_id       = p.id
   );

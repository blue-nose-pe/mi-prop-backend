-- ====================================================================
-- Cliente (Observaciones plataforma, 2026-05-29) — ajustes de visibilidad
-- en el menu lateral para los roles Asesor y Coordinador:
--
--   1. Asesor en seccion "Colegios": debe ver solo "Ver Pruebas por
--      Colegio" y "Ver Colegios". NO debe ver "Crear". El item "Crear"
--      del drawer esta gateado con `db_users.school.write`, que el grupo
--      asesor_permissions tenia desde la migracion 014. Lo removemos.
--      Coherente con la observacion del cliente: el asesor opera sus
--      colegios pero no los crea (admin only).
--
--   2. Coordinador en seccion "Estudiantes": debe ver "Actualizar"
--      (segun observacion explicita del cliente). Los items "Crear" y
--      "Actualizar" estan gateados con `db_users.users.write`. El grupo
--      coordinador_permissions no lo tenia; lo agregamos. Resultado:
--      coord vera Crear + Actualizar (cliente menciono Actualizar; Crear
--      es secuela aceptada del mismo permission code).
--
-- Idempotente — usa NOT EXISTS / DELETE-by-name. Re-ejecutable sin
-- efectos colaterales.
-- ====================================================================

SET QUOTED_IDENTIFIER ON;

-- 1) Quitar db_users.school.write del grupo asesor_permissions.
DELETE pgp
  FROM permission_group_permission pgp
  JOIN permission_group  g ON g.id = pgp.permission_group_id
  JOIN permission        p ON p.id = pgp.permission_id
 WHERE g.code = 'asesor_permissions'
   AND p.code = 'db_users.school.write';

-- 2) Agregar db_users.users.write al grupo coordinador_permissions.
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'coordinador_permissions'
   AND p.code = 'db_users.users.write'
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission existing
      WHERE existing.permission_group_id = g.id
        AND existing.permission_id       = p.id
   );

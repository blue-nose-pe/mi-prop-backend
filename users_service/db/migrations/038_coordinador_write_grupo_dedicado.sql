-- ====================================================================
-- Decisión del cliente (2026-07-01): gestionar coordinadores debe ser
-- SELECTIVO por asesor, no default de todos. La migr 037 dejó
-- db_users.coordinador.write horneado en asesor_permissions (los 12
-- asesores lo tenían). Aquí lo movemos a un grupo DEDICADO que el
-- superadmin asigna caso por caso desde "Perfil de acceso":
--
--   coordinador_management — "Gestión de coordinadores"
--
-- Efecto neto: ningún asesor conserva el permiso hasta que el superadmin
-- le agregue este grupo (aditivo: conviven asesor_permissions + este).
-- El enforcement no cambia: admin siempre puede; asesor con el grupo,
-- solo sobre SUS colegios (COORDINADOR_SCOPE). Idempotente.
-- ====================================================================

SET QUOTED_IDENTIFIER ON;

-- 1) Alta del grupo dedicado (MERGE — no duplica si ya existe).
MERGE INTO permission_group AS target
USING (VALUES
  ('coordinador_management', 'Gestión de coordinadores',
   'Asignar/quitar coordinadores de colegios. El asesor solo sobre sus propios colegios. Lo otorga el superadmin caso por caso.')
) AS source (code, name, description)
ON target.code = source.code
WHEN NOT MATCHED THEN
    INSERT (code, name, description)
    VALUES (source.code, source.name, source.description);

-- 2) Vincular db_users.coordinador.write al grupo dedicado.
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'coordinador_management'
   AND p.code = 'db_users.coordinador.write'
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission existing
      WHERE existing.permission_group_id = g.id
        AND existing.permission_id       = p.id
   );

-- 3) Retirar el permiso del grupo base de asesores (revierte el default
--    ON de la migr 037; los asesores lo recuperan solo vía el grupo nuevo).
DELETE pgp
  FROM permission_group_permission pgp
  JOIN permission_group g ON g.id = pgp.permission_group_id
  JOIN permission       p ON p.id = pgp.permission_id
 WHERE g.code = 'asesor_permissions'
   AND p.code = 'db_users.coordinador.write';

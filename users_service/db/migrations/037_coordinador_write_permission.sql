-- ====================================================================
-- Cliente (Observaciones plataforma, L53): el rol ASESOR necesita poder
-- ACTUALIZAR los coordinadores de sus colegios. Hasta ahora AssignCoordinador/
-- RevokeCoordinador eran solo-superadmin (hardcodeado). En vez de abrir eso a
-- todos los asesores, creamos un PERMISO dedicado que el superadmin asigna a
-- quien quiera (el sistema de permisos solo lo toca el superadmin).
--
--   db_users.coordinador.write — gestionar coordinadores de un colegio.
--
-- El back (permmw + PermissionMap) exige este permiso, y ademas SCOPEA: el
-- asesor solo puede tocar coordinadores de SUS colegios (superadmin, todos).
-- Por defecto se otorga al grupo asesor_permissions (el superadmin puede
-- quitarlo o mover asesores a un grupo restringido). Idempotente / re-ejecutable.
-- ====================================================================

SET QUOTED_IDENTIFIER ON;

-- 1) Alta del permiso en el catalogo (MERGE — no duplica si ya existe).
MERGE INTO permission AS target
USING (VALUES
  ('db_users', 'db_users.coordinador.write', 'Gestionar coordinadores',
   'Asignar/quitar coordinadores de un colegio (el asesor, scopeado a sus colegios)')
) AS source (scope, code, name, description)
ON target.code = source.code
WHEN MATCHED THEN
    UPDATE SET name = source.name, scope = source.scope, description = source.description
WHEN NOT MATCHED THEN
    INSERT (scope, code, name, description)
    VALUES (source.scope, source.code, source.name, source.description);

-- 2) Otorgar el permiso al grupo asesor_permissions (default ON para asesores;
--    el superadmin puede removerlo o crear un grupo restringido).
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'asesor_permissions'
   AND p.code = 'db_users.coordinador.write'
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission existing
      WHERE existing.permission_group_id = g.id
        AND existing.permission_id       = p.id
   );

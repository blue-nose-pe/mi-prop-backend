-- Cliente: 2 permisos nuevos para gatear las vistas que se entregaron
-- en la fase 2 del feedback:
--
--   analytics.simulacro_masivo.read  → ve el apartado "Simulacro Masivo"
--                                       (solo rol Marketing)
--   analytics.colegio_portal.read    → ve el "Portal del Colegio"
--                                       (cuenta del colegio cliente)
--
-- Idempotente — MERGE sobre `permission`.
--
-- Tambien crea los 2 permission_groups de soporte (marketing y colegio)
-- si no existen, y los asocia con sus permisos. Asi UCSP puede crear
-- usuarios para Marketing / cuenta del Colegio y asignarles directamente
-- el grupo correspondiente sin armar la matriz a mano.

SET QUOTED_IDENTIFIER ON;

-- 1) MERGE de los 2 permisos nuevos.
MERGE INTO permission AS target
USING (VALUES
  ('analytics', 'analytics.simulacro_masivo.read',
     'Read Simulacro Masivo panel',
     'Ver el apartado Simulacro Masivo (campania publica con HubSpot). Tipicamente solo Marketing.'),
  ('analytics', 'analytics.colegio_portal.read',
     'Read Colegio Portal',
     'Ver el portal del colegio (dashboard de progreso + descarga de informe). Tipicamente cuentas del rol Colegio.')
) AS source (scope, code, name, description)
ON target.code = source.code
WHEN MATCHED THEN
    UPDATE SET name = source.name, scope = source.scope, description = source.description
WHEN NOT MATCHED THEN
    INSERT (scope, code, name, description)
    VALUES (source.scope, source.code, source.name, source.description);

-- 2) MERGE de los 2 permission_groups si no existen.
MERGE INTO permission_group AS target
USING (VALUES
  ('marketing_permissions', 'Marketing', 'Equipo de Marketing UCSP. Acceso al apartado Simulacro Masivo + reporting de campania.'),
  ('colegio_permissions',   'Colegio',   'Cuenta del colegio cliente. Acceso al portal del colegio + descarga de informe.')
) AS source (code, name, description)
ON target.code = source.code
WHEN NOT MATCHED THEN
    INSERT (code, name, description)
    VALUES (source.code, source.name, source.description);

-- 3) Asocia permisos a los grupos.
-- Marketing: ve simulacro masivo + dashboards generales + exports.
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'marketing_permissions'
   AND p.code IN (
     'analytics.simulacro_masivo.read',
     'analytics.dashboard.read',
     'analytics.export.write',
     'db_users.users.read',
     'db_users.school.read',
     'db_keys.key.read'
   )
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission gp
      WHERE gp.permission_group_id = g.id AND gp.permission_id = p.id
   );

-- Colegio: ve solo el portal del colegio + lectura de sus alumnos
-- (filtrado a su school_id por el back).
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'colegio_permissions'
   AND p.code IN (
     'analytics.colegio_portal.read',
     'db_users.users.read',
     'db_users.school.read'
   )
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission gp
      WHERE gp.permission_group_id = g.id AND gp.permission_id = p.id
   );

-- 4) Adicional: ambos permisos nuevos los agregamos a admin_permissions
-- (asi el admin general puede ver todas las pantallas sin gymnastics
-- adicionales).
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'admin_permissions'
   AND p.code IN ('analytics.simulacro_masivo.read', 'analytics.colegio_portal.read')
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission gp
      WHERE gp.permission_group_id = g.id AND gp.permission_id = p.id
   );

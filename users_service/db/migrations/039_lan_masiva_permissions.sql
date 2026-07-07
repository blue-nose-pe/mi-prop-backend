-- ====================================================================
-- Cliente: separar la LAN MASIVA (campaña "Prepárate", llaves sin colegio que
-- captan leads) del simulacro de colegio, con permisos PROPIOS. Hasta ahora
-- crear/editar una LAN montaba sobre el generico db_keys.key.write (el mismo de
-- las llaves de colegio), asi que no se podia dar "crear llaves de colegio" sin
-- dar tambien "crear LAN", y un asesor podia crear LANs sin querer.
--
--   db_keys.lan.read  — ver/listar las llaves LAN masivas.
--   db_keys.lan.write — crear/editar/desactivar las llaves LAN masivas.
--
-- Regla simple pedida por el cliente: para CREAR/EDITAR una LAN hace falta
-- lan.write; para LEERLAS, lan.read. El back gatea por mode='lan'.
--
-- Por defecto ambos se otorgan SOLO al grupo admin_permissions (Admin UCSP).
-- El superadmin (o quien gestione grupos) los asigna a Marketing u otros roles
-- desde la gestion de grupos de permisos. Idempotente / re-ejecutable.
-- ====================================================================

SET QUOTED_IDENTIFIER ON;

-- 1) Alta de los permisos en el catalogo (MERGE — no duplica).
MERGE INTO permission AS target
USING (VALUES
  ('db_keys', 'db_keys.lan.read',  'Ver LAN masivas',
   'Ver/listar las llaves de simulacro masivo (LAN, campana Preparate)'),
  ('db_keys', 'db_keys.lan.write', 'Gestionar LAN masivas',
   'Crear, editar y desactivar las llaves de simulacro masivo (LAN)')
) AS source (scope, code, name, description)
ON target.code = source.code
WHEN MATCHED THEN
    UPDATE SET name = source.name, scope = source.scope, description = source.description
WHEN NOT MATCHED THEN
    INSERT (scope, code, name, description)
    VALUES (source.scope, source.code, source.name, source.description);

-- 2) Otorgar ambos permisos al grupo admin_permissions (default ON para Admin;
--    el superadmin puede otorgarlos a Marketing u otros grupos). Idempotente.
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'admin_permissions'
   AND p.code IN ('db_keys.lan.read', 'db_keys.lan.write')
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission existing
      WHERE existing.permission_group_id = g.id
        AND existing.permission_id       = p.id
   );

-- Permisos para administración de permission_group via API.
-- El admin con db_users.permission_group.* puede CRUD grupos a runtime.
MERGE INTO permission AS target
USING (VALUES
  ('db_users', 'db_users.permission_group.read',  'Listar y consultar permission_group'),
  ('db_users', 'db_users.permission_group.write', 'Crear, editar, borrar permission_group y asignar permisos a grupos'),
  ('db_users', 'db_users.permission.read',        'Listar el catálogo de permisos individuales')
) AS source (scope, code, description)
ON target.code = source.code
WHEN MATCHED THEN
    UPDATE SET description = source.description
WHEN NOT MATCHED THEN
    INSERT (scope, code, name, description, active)
    VALUES (source.scope, source.code, source.code, source.description, 1);

-- Asignar los nuevos codes al grupo admin_permissions (que ya existe).
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'admin_permissions'
   AND p.code IN ('db_users.permission_group.read', 'db_users.permission_group.write', 'db_users.permission.read')
   AND NOT EXISTS (
       SELECT 1 FROM permission_group_permission existing
        WHERE existing.permission_group_id = g.id
          AND existing.permission_id       = p.id
   );

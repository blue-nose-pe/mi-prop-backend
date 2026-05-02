-- Seed idempotente: catálogo de permisos + grupo de ejemplo.
-- Usa MERGE para que re-aplicar la migración no falle ni duplique.

MERGE INTO permission AS target
USING (VALUES
    -- users
    ('users',      'users.view',        'View users',         'List and view detail'),
    ('users',      'users.create',      'Create users',       'Register new users'),
    ('users',      'users.edit',        'Edit users',         'Modify user data'),
    ('users',      'users.delete',      'Delete users',       'Deactivate or delete users'),
    ('users',      'users.permissions', 'Assign permissions', 'Grant or revoke permissions'),
    -- permission (CRUD sobre el catálogo de permisos)
    ('permission', 'permission.view',   'View permissions',   'List and view the permissions catalog'),
    ('permission', 'permission.create', 'Create permissions', 'Register new permissions'),
    ('permission', 'permission.edit',   'Edit permissions',   'Modify existing permissions'),
    ('permission', 'permission.delete', 'Delete permissions', 'Deactivate or delete permissions'),
    -- colegios
    ('colegios',   'colegios.view',     'View schools',       'List and view schools'),
    ('colegios',   'colegios.create',   'Create schools',     'Register new schools'),
    ('colegios',   'colegios.edit',     'Edit schools',       'Edit school data'),
    ('colegios',   'colegios.delete',   'Delete schools',     'Delete or deactivate schools')
) AS source (scope, code, name, description)
ON target.code = source.code
WHEN MATCHED THEN
    UPDATE SET name  = source.name,
               scope = source.scope
WHEN NOT MATCHED THEN
    INSERT (scope, code, name, description)
    VALUES (source.scope, source.code, source.name, source.description);

-- Grupo de ejemplo: student_permissions.
MERGE INTO permission_group AS target
USING (VALUES
    ('student_permissions', 'Permissions for students', 'users.view, users.create, colegios.view')
) AS source (code, name, description)
ON target.code = source.code
WHEN MATCHED THEN
    UPDATE SET name = source.name
WHEN NOT MATCHED THEN
    INSERT (code, name, description)
    VALUES (source.code, source.name, source.description);

-- Asignar los 3 permisos del grupo (idempotente vía NOT EXISTS).
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'student_permissions'
   AND p.code IN ('users.view', 'users.create', 'colegios.view')
   AND NOT EXISTS (
       SELECT 1 FROM permission_group_permission existing
        WHERE existing.permission_group_id = g.id
          AND existing.permission_id       = p.id
   );

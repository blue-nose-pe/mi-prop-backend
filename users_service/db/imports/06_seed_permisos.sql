-- Run connected to db_users.
-- Seed: permissions + example permission group

INSERT INTO permission (scope, code, name, description) VALUES
  -- users
  ('users',      'users.view',        'View users',         'List and view detail'),
  ('users',      'users.create',      'Create users',       'Register new users'),
  ('users',      'users.edit',        'Edit users',         'Modify user data'),
  ('users',      'users.delete',      'Delete users',       'Deactivate or delete users'),
  ('users',      'users.permissions', 'Assign permissions', 'Grant or revoke permissions'),
  -- permission (CRUD over the permission catalog itself)
  ('permission', 'permission.view',   'View permissions',   'List and view the permissions catalog'),
  ('permission', 'permission.create', 'Create permissions', 'Register new permissions'),
  ('permission', 'permission.edit',   'Edit permissions',   'Modify existing permissions'),
  ('permission', 'permission.delete', 'Delete permissions', 'Deactivate or delete permissions'),
  -- colegios
  ('colegios',   'colegios.view',     'View schools',       'List and view schools'),
  ('colegios',   'colegios.create',   'Create schools',     'Register new schools'),
  ('colegios',   'colegios.edit',     'Edit schools',       'Edit school data'),
  ('colegios',   'colegios.delete',   'Delete schools',     'Delete or deactivate schools')
ON CONFLICT (code) DO UPDATE
  SET name  = EXCLUDED.name,
      scope = EXCLUDED.scope;

-- Example group: student_permissions (users.view, users.create, colegios.view)
INSERT INTO permission_group (code, name, description) VALUES
  ('student_permissions', 'Permissions for students', 'users.view, users.create, colegios.view')
ON CONFLICT (code) DO UPDATE
  SET name = EXCLUDED.name;

INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'student_permissions'
   AND p.code IN ('users.view', 'users.create', 'colegios.view')
ON CONFLICT (permission_group_id, permission_id) DO NOTHING;

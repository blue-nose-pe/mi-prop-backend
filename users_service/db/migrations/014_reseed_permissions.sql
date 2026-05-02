-- ====================================================================
-- Reseed del catálogo de permisos al formato `<service>.<tabla>.<acción>`.
-- ====================================================================
--
-- Razón: el formato anterior (`users.view`, `colegios.edit`, etc.) no
-- discriminaba a qué TABLA aplicaba. Mi Propósito 2.0 tiene 6 servicios
-- con BDs separadas → necesitamos granularidad por (servicio, tabla).
--
-- Convención:
--   <service>.<table>.<action>
--     service  = scope/microservicio (db_users, db_exams, db_keys,
--                db_satisfaction, hubspot, analytics)
--     table    = nombre de la tabla destino (o "dashboard"/"export"
--                para servicios sin tablas como analytics)
--     action   = read | write    (granularidad puede crecer sin romper)
--
-- Ejemplos:
--   db_users.users.read              ← listar y ver detalle de users
--   db_users.users.write             ← crear/editar/desactivar users
--   db_users.permission.write        ← editar el catálogo de permisos
--                                      (solo superadmin lo necesita
--                                      explícitamente; superadmin ya
--                                      bypassa pero también puede
--                                      delegar el permiso)
--   db_exams.exam.write              ← crear/publicar/clonar exams
--   hubspot.contact.write            ← upsert de contacts en HS
--
-- IMPORTANTE: superadmins (users.is_superadmin = 1) BYPASSAN este
-- catálogo — siempre tienen acceso. La tabla `permission` solo aplica
-- a NO-superadmins.
--
-- Esta migración:
--   1. Elimina los permisos viejos (formato `users.view`, etc.) que
--      ya no existen en el código y por tanto ningún caller los chequea.
--   2. MERGE-ea el catálogo nuevo (idempotente).
--   3. Reasigna el grupo "student_permissions" con los códigos nuevos.

-- ----- 1) Limpiar el catálogo viejo (solo si efectivamente está) -----
DELETE FROM permission_group_permission
 WHERE permission_id IN (
   SELECT id FROM permission
    WHERE code IN (
      'users.view','users.create','users.edit','users.delete','users.permissions',
      'permission.view','permission.create','permission.edit','permission.delete',
      'colegios.view','colegios.create','colegios.edit','colegios.delete'
    )
 );

DELETE FROM permission
 WHERE code IN (
   'users.view','users.create','users.edit','users.delete','users.permissions',
   'permission.view','permission.create','permission.edit','permission.delete',
   'colegios.view','colegios.create','colegios.edit','colegios.delete'
 );

-- ----- 2) MERGE del catálogo nuevo -----
MERGE INTO permission AS target
USING (VALUES
  -- ───── db_users ─────
  ('db_users', 'db_users.users.read',                'Read users',                'List and view detail of users'),
  ('db_users', 'db_users.users.write',               'Write users',               'Create, update, deactivate users'),
  ('db_users', 'db_users.school.read',               'Read schools',              'List and view schools'),
  ('db_users', 'db_users.school.write',              'Write schools',             'Create, update, deactivate schools'),
  ('db_users', 'db_users.permission.read',           'Read permission catalog',   'Inspect the permission catalog (table `permission`)'),
  ('db_users', 'db_users.permission.write',          'Write permission catalog',  'Edit the permission catalog (only superadmins typically)'),
  ('db_users', 'db_users.permission_group.read',     'Read permission groups',    'List permission groups'),
  ('db_users', 'db_users.permission_group.write',    'Write permission groups',   'Create groups, attach/detach permissions, assign to users'),
  ('db_users', 'db_users.assignment.read',           'Read assignments',          'See current and historical asesor/colegio/coordinador assignments'),
  ('db_users', 'db_users.assignment.write',          'Write assignments',         'Reassign asesor/colegio/coordinador (preserves history)'),
  ('db_users', 'db_users.audit_log.read',            'Read audit log',            'Read users-service audit log'),

  -- ───── db_exams ─────
  ('db_exams', 'db_exams.exam_type.read',            'Read exam types',           'List exam types catalog (vocacional/simulacro/habitos)'),
  ('db_exams', 'db_exams.exam.read',                 'Read exams',                'Search and view exams'),
  ('db_exams', 'db_exams.exam.write',                'Write exams',               'Create, update, publish, deactivate, clone exams'),
  ('db_exams', 'db_exams.question.read',             'Read questions',            'Read the question bank'),
  ('db_exams', 'db_exams.question.write',            'Write questions',           'Create, edit, deactivate questions and options'),
  ('db_exams', 'db_exams.exam_question.write',       'Write exam_question',       'Add/remove/reorder questions inside an exam'),
  ('db_exams', 'db_exams.exam_attempt.read',         'Read attempts',             'List and inspect exam attempts'),
  ('db_exams', 'db_exams.exam_attempt.write',        'Write attempts',            'Start/answer/finish attempts on behalf of users (admin)'),
  ('db_exams', 'db_exams.audit_log.read',            'Read audit log',            'Read exams-service audit log'),

  -- ───── db_keys ─────
  ('db_keys',  'db_keys.key.read',                   'Read keys',                 'List and inspect access keys'),
  ('db_keys',  'db_keys.key.write',                  'Write keys',                'Generate, edit and deactivate keys'),
  ('db_keys',  'db_keys.key_usage.read',             'Read key usage',            'Read key_usage audit (who used which key when)'),
  ('db_keys',  'db_keys.audit_log.read',             'Read audit log',            'Read keys-service audit log'),

  -- ───── db_satisfaction ─────
  ('db_satisfaction', 'db_satisfaction.survey.read',           'Read surveys',     'List and view surveys'),
  ('db_satisfaction', 'db_satisfaction.survey.write',          'Write surveys',    'Create, edit, publish, deactivate surveys and questions'),
  ('db_satisfaction', 'db_satisfaction.survey_response.read',  'Read responses',   'Read survey responses (aggregated and individual)'),
  ('db_satisfaction', 'db_satisfaction.survey_response.write', 'Write responses',  'Submit responses on behalf of users (admin)'),
  ('db_satisfaction', 'db_satisfaction.audit_log.read',        'Read audit log',   'Read satisfaction-service audit log'),

  -- ───── hubspot (no DB; opera sobre HubSpot CRM) ─────
  ('hubspot',  'hubspot.contact.read',               'Read HubSpot contacts',     'Lookup contacts by DNI or record_id'),
  ('hubspot',  'hubspot.contact.write',              'Write HubSpot contacts',    'Upsert contacts and properties'),
  ('hubspot',  'hubspot.otp.write',                  'Trigger OTP webhook',       'Send OTP via HubSpot Automation webhook'),
  ('hubspot',  'hubspot.custom_object.write',        'Write custom objects',      'Upsert Key/Asesor/Colegio in HubSpot'),
  ('hubspot',  'hubspot.jobs.read',                  'Read failed sync jobs',     'List jobs in DLQ (admin)'),
  ('hubspot',  'hubspot.jobs.write',                 'Retry failed sync jobs',    'Retry jobs from DLQ (admin)'),

  -- ───── analytics (sin DB; agregador) ─────
  ('analytics','analytics.dashboard.read',           'Read dashboards',           'Read aggregated dashboards (asesor/colegio/estudiante/comparativo)'),
  ('analytics','analytics.export.write',             'Generate XLSX exports',     'Generate Excel exports of dashboards')
) AS source (scope, code, name, description)
ON target.code = source.code
WHEN MATCHED THEN
    UPDATE SET name = source.name, scope = source.scope, description = source.description
WHEN NOT MATCHED THEN
    INSERT (scope, code, name, description)
    VALUES (source.scope, source.code, source.name, source.description);

-- ----- 3) Re-poblar el grupo `student_permissions` con códigos nuevos -----
-- (ya existe del seed 008; conservamos el mismo `code` del grupo)
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'student_permissions'
   AND p.code IN (
     'db_users.users.read',
     'db_users.school.read',
     'db_exams.exam.read',
     'db_exams.exam_attempt.write',  -- el alumno crea attempts
     'db_satisfaction.survey.read',
     'db_satisfaction.survey_response.write'
   )
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission existing
      WHERE existing.permission_group_id = g.id
        AND existing.permission_id       = p.id
   );

-- ----- 4) Crear grupos canónicos para roles típicos (idempotente) -----
MERGE INTO permission_group AS target
USING (VALUES
  ('admin_permissions',        'Admin UCSP',        'CRUD completo sobre todas las tablas operacionales (no edita el catálogo de permisos — eso es del superadmin).'),
  ('asesor_permissions',       'Asesor comercial',  'Lectura amplia + write en sus colegios y keys + ver dashboards de sus asignaciones.'),
  ('coordinador_permissions',  'Coordinador colegio','Lectura del colegio asignado + sus estudiantes + sus resultados.')
) AS source (code, name, description)
ON target.code = source.code
WHEN MATCHED THEN
    UPDATE SET name = source.name, description = source.description
WHEN NOT MATCHED THEN
    INSERT (code, name, description)
    VALUES (source.code, source.name, source.description);

-- admin_permissions: TODO menos `db_users.permission.write` (que es exclusivo
-- del superadmin por convención — aunque puede dárselo manualmente).
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'admin_permissions'
   AND p.code <> 'db_users.permission.write'
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission existing
      WHERE existing.permission_group_id = g.id
        AND existing.permission_id       = p.id
   );

-- asesor_permissions: read amplio + write en colegios/keys + dashboards.
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'asesor_permissions'
   AND p.code IN (
     'db_users.users.read',
     'db_users.school.read', 'db_users.school.write',
     'db_users.assignment.read',
     'db_exams.exam_type.read', 'db_exams.exam.read',
     'db_exams.exam_attempt.read',
     'db_keys.key.read', 'db_keys.key.write', 'db_keys.key_usage.read',
     'db_satisfaction.survey.read',
     'db_satisfaction.survey_response.read',
     'hubspot.contact.read',
     'analytics.dashboard.read', 'analytics.export.write'
   )
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission existing
      WHERE existing.permission_group_id = g.id
        AND existing.permission_id       = p.id
   );

-- coordinador_permissions: lectura sobre su colegio + sus estudiantes.
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'coordinador_permissions'
   AND p.code IN (
     'db_users.users.read',
     'db_users.school.read',
     'db_users.assignment.read',
     'db_exams.exam_type.read', 'db_exams.exam.read',
     'db_exams.exam_attempt.read',
     'db_keys.key.read',
     'db_satisfaction.survey.read',
     'db_satisfaction.survey_response.read',
     'analytics.dashboard.read'
   )
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission existing
      WHERE existing.permission_group_id = g.id
        AND existing.permission_id       = p.id
   );

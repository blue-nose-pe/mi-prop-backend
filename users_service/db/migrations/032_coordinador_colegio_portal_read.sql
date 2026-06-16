-- ====================================================================
-- Manual de Gestion de Accesos (v1.0, Jun 2026) — el COORDINADOR de colegio
-- tiene "vista acotada, solo su colegio". Hoy NO podia entrar al "Portal del
-- Colegio" porque coordinador_permissions no tenia
-- analytics.colegio_portal.read (el guard de ruta /app/colegio-portal y el
-- item del drawer lo bloqueaban). Su dashboard ademas salia vacio porque el
-- scope solo resolvia colegios via assignment 'asesor_de_colegio'.
--
-- Otorgamos analytics.colegio_portal.read a coordinador_permissions. El
-- alcance (solo SU colegio) lo garantiza el server, igual que para el asesor
-- (migration 027), gracias a que school_repo.ListByAsesor ahora tambien
-- resuelve el colegio que el usuario COORDINA (school.user_id = caller):
--   * Front (colegio-portal.component): el selector se llena con
--     listSchoolsForAsesor → solo el/los colegio(s) del coordinador.
--   * Back (gateway enforceColegioScope): valida que el colegio este en
--     ListSchoolsByAsesor del caller; si no, 403. Un coordinador no puede
--     ver el dashboard de un colegio ajeno ni manipulando la request.
--
-- Idempotente — NOT EXISTS. Re-ejecutable sin efectos colaterales.
-- ====================================================================

SET QUOTED_IDENTIFIER ON;

INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'coordinador_permissions'
   AND p.code = 'analytics.colegio_portal.read'
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission existing
      WHERE existing.permission_group_id = g.id
        AND existing.permission_id       = p.id
   );

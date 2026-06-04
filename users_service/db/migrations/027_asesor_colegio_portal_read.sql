-- ====================================================================
-- Cliente (2026-06-04) — el ASESOR debe poder entrar al "Portal del
-- Colegio", pero viendo SOLO sus colegios asignados (no todos).
--
-- Estado previo: asesor_permissions NO tenia analytics.colegio_portal.read,
-- asi que el guard de ruta /app/colegio-portal y el item del drawer
-- "Portal del Colegio" lo bloqueaban por completo.
--
-- Otorgamos analytics.colegio_portal.read a asesor_permissions. El alcance
-- por colegio ya esta garantizado sin codigo extra:
--   * Front (colegio-portal.component): el selector se llena con
--     listSchoolsForAsesor → solo los colegios del asesor.
--   * Back (gateway enforceColegioScope): GET
--     /api/analytics/colegio/{id}/dashboard valida que el colegio este en
--     ListSchoolsByAsesor del caller; si no, 403. Asi un asesor no puede
--     ver el dashboard de un colegio que no es suyo ni manipulando la
--     request.
--
-- Idempotente — NOT EXISTS. Re-ejecutable sin efectos colaterales.
-- ====================================================================

SET QUOTED_IDENTIFIER ON;

INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'asesor_permissions'
   AND p.code = 'analytics.colegio_portal.read'
   AND NOT EXISTS (
     SELECT 1 FROM permission_group_permission existing
      WHERE existing.permission_group_id = g.id
        AND existing.permission_id       = p.id
   );

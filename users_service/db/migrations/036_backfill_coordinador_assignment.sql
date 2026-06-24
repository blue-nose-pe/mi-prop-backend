-- Many-to-many de coordinadores: backfill. Hasta ahora el "coordinador" de un
-- colegio era school.user_id (1 por colegio). Ahora los coordinadores se
-- manejan via assignment kind='coordinador_de_colegio' (source=coordinador,
-- target=school.user_id), permitiendo VARIOS por colegio.
--
-- Este backfill crea, para cada colegio con user_id, la assignment vigente del
-- coordinador actual (source=user_id, target=user_id) si aún no existe — para
-- que el coordinador existente siga viéndose en el nuevo modelo.

SET QUOTED_IDENTIFIER ON;
SET ANSI_NULLS ON;
GO

INSERT INTO assignment (kind, source_user_id, target_user_id, created_by)
SELECT 'coordinador_de_colegio', s.user_id, s.user_id, s.user_id
  FROM school s
 WHERE s.user_id IS NOT NULL
   AND NOT EXISTS (
        SELECT 1 FROM assignment a
         WHERE a.kind = 'coordinador_de_colegio'
           AND a.source_user_id = s.user_id
           AND a.target_user_id = s.user_id
           AND a.valid_to IS NULL
   );
GO

-- Índice para listar coordinadores vigentes de un colegio por (target, valid_to).
-- (idx_assignment_target_kind_active ya cubre (target_user_id, kind, valid_to).)
GO

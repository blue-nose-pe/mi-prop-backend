-- Histórico de reasignaciones (asesor↔colegio↔coordinador↔estudiante).
-- Patrón "assignment con valid_from/valid_to" (SCD-tipo-2 ligero).
--
-- Reglas:
--   - Insertar una asignación con valid_to = NULL la marca como "vigente".
--   - Reasignar = cerrar la vigente (set valid_to = NOW()) e insertar una nueva.
--   - El histórico nunca se actualiza para "pisar" — solo se cierra y nace otro.
--
-- Permite responder: "¿quién era el asesor del colegio X el 2024-03-15?".

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'assignment')
BEGIN
    CREATE TABLE assignment (
        id              UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_assignment_id DEFAULT NEWID() PRIMARY KEY,
        kind            NVARCHAR(48)     NOT NULL,
            -- valores válidos:
            --   'asesor_de_colegio'        : source = asesor user_id, target = school user_id
            --   'coordinador_de_colegio'   : source = coordinador user_id, target = school user_id
            --   'estudiante_en_colegio'    : source = estudiante user_id, target = school user_id
            --   'asesor_de_estudiante'     : source = asesor user_id, target = estudiante user_id
        source_user_id  UNIQUEIDENTIFIER NOT NULL,
        target_user_id  UNIQUEIDENTIFIER NOT NULL,
        valid_from      DATETIME2(7)     NOT NULL CONSTRAINT df_assignment_valid_from DEFAULT SYSUTCDATETIME(),
        valid_to        DATETIME2(7)     NULL,                 -- NULL = vigente
        created_by      UNIQUEIDENTIFIER NULL,                  -- user_id que hizo el cambio (audit hint)
        created_at      DATETIME2(7)     NOT NULL CONSTRAINT df_assignment_created_at DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_assignment_source FOREIGN KEY (source_user_id) REFERENCES users(id),
        CONSTRAINT fk_assignment_target FOREIGN KEY (target_user_id) REFERENCES users(id),
        CONSTRAINT ck_assignment_kind CHECK (kind IN (
            'asesor_de_colegio',
            'coordinador_de_colegio',
            'estudiante_en_colegio',
            'asesor_de_estudiante'
        ))
    );
END;

-- Lookup más común: "¿quién es el asesor vigente del colegio X?"
-- → WHERE target_user_id = X AND kind = 'asesor_de_colegio' AND valid_to IS NULL.
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_assignment_target_kind_active' AND object_id = OBJECT_ID('assignment'))
    CREATE INDEX idx_assignment_target_kind_active
        ON assignment (target_user_id, kind, valid_to)
        WHERE valid_to IS NULL;

-- Lookup "histórico por colegio".
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_assignment_target_kind' AND object_id = OBJECT_ID('assignment'))
    CREATE INDEX idx_assignment_target_kind
        ON assignment (target_user_id, kind, valid_from);

-- Lookup "asignaciones activas de un asesor".
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_assignment_source_kind_active' AND object_id = OBJECT_ID('assignment'))
    CREATE INDEX idx_assignment_source_kind_active
        ON assignment (source_user_id, kind, valid_to)
        WHERE valid_to IS NULL;

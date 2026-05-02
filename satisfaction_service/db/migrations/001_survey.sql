-- Tabla: survey. Una encuesta puede tener múltiples versiones publicadas
-- (version + active) y dispararse en distintos triggers.
--
-- target_role: a quién se le aplica (admin, asesor, coordinador, estudiante).
-- trigger:     post_test → al cerrar un attempt.
--              on_demand → desde un botón en el front.
--              recurring → cron periódico.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'survey')
BEGIN
    CREATE TABLE survey (
        id            UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_survey_id DEFAULT NEWID() PRIMARY KEY,
        code          NVARCHAR(64)     NOT NULL,
        title         NVARCHAR(255)    NOT NULL,
        description   NVARCHAR(MAX)    NULL,
        target_role   NVARCHAR(32)     NOT NULL,
        trigger_kind  NVARCHAR(32)     NOT NULL,
        version       INT              NOT NULL CONSTRAINT df_survey_version DEFAULT 1,
        published     BIT              NOT NULL CONSTRAINT df_survey_published DEFAULT 0,
        active        BIT              NOT NULL CONSTRAINT df_survey_active DEFAULT 1,
        created_at    DATETIME2(7)     NOT NULL CONSTRAINT df_survey_created_at DEFAULT SYSUTCDATETIME(),
        updated_at    DATETIME2(7)     NULL,
        CONSTRAINT uk_survey_code_version UNIQUE (code, version),
        CONSTRAINT ck_survey_target CHECK (target_role IN ('admin','asesor','coordinador','estudiante')),
        CONSTRAINT ck_survey_trigger CHECK (trigger_kind IN ('post_test','on_demand','recurring'))
    );
END;

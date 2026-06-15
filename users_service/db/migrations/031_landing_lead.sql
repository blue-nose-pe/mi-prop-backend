-- Tabla: landing_lead (lead de la landing publica "Preparate" / simulacro
-- masivo). Replica el embudo de captacion de prod: la landing publica capta
-- un lead ANONIMO (sin cuenta) que luego rinde el simulacro UCSP.
--
-- Modelo APPEND, NO upsert: en prod un mismo DNI se reinscribe por campaña
-- (lead/lead2..lead6/lead_2024 historicos; leads_agrupados = vista dedup).
-- Por eso NO hay constraint unique sobre dni/email — cada inscripcion es una
-- fila. La deduplicacion es responsabilidad de la lectura/reporte.
--
-- Nota: 'lead' es palabra reservada en T-SQL (funcion analitica LEAD) — por
-- eso la tabla se llama landing_lead.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'landing_lead')
BEGIN
    CREATE TABLE landing_lead (
        id                UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_landing_lead_id DEFAULT NEWID() PRIMARY KEY,
        first_name        NVARCHAR(120)    NOT NULL,
        last_name         NVARCHAR(120)    NOT NULL,
        dni               NVARCHAR(20)     NULL,
        phone             NVARCHAR(30)     NULL,
        email             NVARCHAR(160)    NOT NULL,
        -- anio de egreso del colegio (select de la landing, 2015..2027).
        -- NULL = no especificado.
        graduation_year   INT              NULL,
        -- colegio de procedencia: texto LIBRE que escribe el alumno (en prod
        -- vive en temp_*_lan, no en la tabla lead). "" / NULL = sin registrar.
        school_text       NVARCHAR(200)    NULL,
        -- origen del lead. En prod siempre 'landing Simulacro'.
        origen            NVARCHAR(60)     NOT NULL CONSTRAINT df_landing_lead_origen DEFAULT 'landing Simulacro',
        -- key_code de la campaña masiva (LAN) con la que rinde el simulacro.
        key_code          NVARCHAR(64)     NULL,
        -- consentimientos de la landing (Ley 29733). bit 0/1.
        terms_accepted    BIT              NOT NULL CONSTRAINT df_landing_lead_terms DEFAULT 0,
        data_processing   BIT              NOT NULL CONSTRAINT df_landing_lead_data  DEFAULT 0,
        -- record_id que devuelve HubSpot tras el upsert del contacto. "" = aun
        -- no sincronizado.
        hubspot_record_id NVARCHAR(64)     NULL,
        created_at        DATETIME2(7)     NOT NULL CONSTRAINT df_landing_lead_created DEFAULT SYSUTCDATETIME()
    );

    CREATE INDEX ix_landing_lead_dni     ON landing_lead(dni);
    CREATE INDEX ix_landing_lead_email   ON landing_lead(email);
    CREATE INDEX ix_landing_lead_created ON landing_lead(created_at DESC);
    CREATE INDEX ix_landing_lead_key     ON landing_lead(key_code);
END;

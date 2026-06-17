-- 033: estado de envío de acceso por lead (panel del simulacro masivo).
-- El staff selecciona leads y les manda el magic-link desde el panel; estas
-- columnas guardan cuándo se envió y con qué key (LAN), para mostrar
-- "enviado/pendiente", evitar doble envío y permitir reenviar a los pendientes.
--   acceso_enviado_at NULL = pendiente.
-- Idempotente (COL_LENGTH guard).

IF COL_LENGTH('landing_lead', 'acceso_enviado_at') IS NULL
    ALTER TABLE landing_lead ADD acceso_enviado_at DATETIME2(7) NULL;

IF COL_LENGTH('landing_lead', 'acceso_key_code') IS NULL
    ALTER TABLE landing_lead ADD acceso_key_code NVARCHAR(64) NULL;

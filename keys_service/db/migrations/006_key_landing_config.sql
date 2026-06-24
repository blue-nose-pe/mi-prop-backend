-- Agrega landing_config a [key]: JSON con el contenido editable de la landing
-- pública "Preparate" para una campaña LAN (mode='lan'). Permite que marketing
-- edite por lanzamiento las fechas/textos visibles (hero_subtitle, fecha, hora,
-- duracion, modalidad, ...) sin redeploy del frontend.
--
-- NULL/'' = la landing usa sus defaults hardcodeados (prod julio 2026). Solo es
-- relevante para keys mode='lan'; en keys de colegio se ignora.
--
-- Lo escribe el panel "Simulacro Masivo" (PATCH /api/keys/{id} con landing_config)
-- y lo lee la landing vía GET /api/public/active-lan-key (key.landing_config).
-- NVARCHAR(MAX) para alojar el JSON completo sin límite práctico.

SET QUOTED_IDENTIFIER ON;
SET ANSI_NULLS ON;
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'[dbo].[key]') AND name = 'landing_config'
)
BEGIN
    ALTER TABLE [key] ADD landing_config NVARCHAR(MAX) NULL;
END;
GO

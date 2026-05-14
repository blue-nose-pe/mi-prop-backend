-- Tabla: visita. Registro de una visita (presencial o remota) que un
-- asesor hace a un colegio. Replica del concepto operativo "visita" de
-- Mi Propósito 1.0.
--
-- status:
--   scheduled  → programada, todavia no se realizo
--   completed  → realizada (el asesor la marca al volver)
--   cancelled  → cancelada antes de la fecha (motivo en notes)
--   no_show    → el dia llegó pero el colegio no estaba disponible
--
-- Indices pensados para los lookups mas comunes:
--   * "visitas del asesor X" (filtra por asesor)
--   * "visitas del colegio Y" (filtra por colegio)
--   * "completadas para el dashboard" (status = completed + asesor)

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'visita')
BEGIN
    CREATE TABLE visita (
        id              UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_visita_id DEFAULT NEWID() PRIMARY KEY,
        asesor_user_id  UNIQUEIDENTIFIER NOT NULL,
        school_id       UNIQUEIDENTIFIER NOT NULL,
        scheduled_at    DATETIME2(7)     NOT NULL,
        completed_at    DATETIME2(7)     NULL,
        status          NVARCHAR(16)     NOT NULL CONSTRAINT df_visita_status DEFAULT 'scheduled',
        notes           NVARCHAR(MAX)    NULL,
        created_at      DATETIME2(7)     NOT NULL CONSTRAINT df_visita_created_at DEFAULT SYSUTCDATETIME(),
        updated_at      DATETIME2(7)     NULL,
        CONSTRAINT ck_visita_status CHECK (status IN ('scheduled', 'completed', 'cancelled', 'no_show')),
        CONSTRAINT fk_visita_asesor FOREIGN KEY (asesor_user_id) REFERENCES users(id),
        CONSTRAINT fk_visita_school FOREIGN KEY (school_id)      REFERENCES school(id)
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_visita_asesor' AND object_id = OBJECT_ID('visita'))
    CREATE INDEX idx_visita_asesor ON visita (asesor_user_id, scheduled_at DESC);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_visita_school' AND object_id = OBJECT_ID('visita'))
    CREATE INDEX idx_visita_school ON visita (school_id, scheduled_at DESC);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_visita_asesor_status' AND object_id = OBJECT_ID('visita'))
    CREATE INDEX idx_visita_asesor_status ON visita (asesor_user_id, status);

IF OBJECT_ID('trg_visita_updated_at', 'TR') IS NOT NULL DROP TRIGGER trg_visita_updated_at;
EXEC('
CREATE TRIGGER trg_visita_updated_at
ON visita
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE v
       SET updated_at = SYSUTCDATETIME()
      FROM visita v
     INNER JOIN inserted i ON v.id = i.id;
END
');

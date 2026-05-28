-- Tabla: key. Códigos de acceso a tests (modos: school o LAN).
-- Reemplaza a las "keys" del P1 (prod_ucsp_simulacro/vocacional/habitos).
--
-- Campos clave:
--   * code         único (alfanumérico, ej: "S2024001" o "LAN##").
--   * exam_type_id catálogo cross-DB → exams_service.exam_type.id (lógico, INT).
--   * mode         "school" → atado a un school_id; "lan" → masivo, school_id = NULL.
--   * grade/section: para reportería.
--   * valid_from/valid_to: ventana temporal (NULL = sin restricción).
--   * max_uses     0 = ilimitado.
--   * current_uses contador atómico que se incrementa al validar.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = '[key]')
BEGIN
    CREATE TABLE [key] (
        id              UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_key_id DEFAULT NEWID() PRIMARY KEY,
        code            NVARCHAR(64)     NOT NULL,
        exam_type_id    INT              NOT NULL,
        school_id       UNIQUEIDENTIFIER NULL,
        asesor_user_id  UNIQUEIDENTIFIER NOT NULL,
        mode            NVARCHAR(16)     NOT NULL,
        grade           NVARCHAR(16)     NULL,
        section         NVARCHAR(16)     NULL,
        valid_from      DATETIME2(7)     NULL,
        valid_to        DATETIME2(7)     NULL,
        max_uses        INT              NOT NULL CONSTRAINT df_key_max_uses DEFAULT 0,
        current_uses    INT              NOT NULL CONSTRAINT df_key_current_uses DEFAULT 0,
        active          BIT              NOT NULL CONSTRAINT df_key_active DEFAULT 1,
        created_at      DATETIME2(7)     NOT NULL CONSTRAINT df_key_created_at DEFAULT SYSUTCDATETIME(),
        updated_at      DATETIME2(7)     NULL,
        CONSTRAINT uk_key_code UNIQUE (code),
        CONSTRAINT ck_key_mode CHECK (mode IN ('school', 'lan'))
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_key_school' AND object_id = OBJECT_ID('[key]'))
    CREATE INDEX idx_key_school ON [key] (school_id);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_key_asesor' AND object_id = OBJECT_ID('[key]'))
    CREATE INDEX idx_key_asesor ON [key] (asesor_user_id);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_key_type_active' AND object_id = OBJECT_ID('[key]'))
    CREATE INDEX idx_key_type_active ON [key] (exam_type_id, active);

IF OBJECT_ID('trg_key_updated_at', 'TR') IS NOT NULL DROP TRIGGER trg_key_updated_at;
EXEC('
CREATE TRIGGER trg_key_updated_at
ON [key]
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE k SET updated_at = SYSUTCDATETIME()
      FROM [key] k INNER JOIN inserted i ON k.id = i.id;
END
');

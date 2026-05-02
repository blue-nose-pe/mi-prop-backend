-- Tabla: school (colegio). Cada school se "representa" por un user.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'school')
BEGIN
    CREATE TABLE school (
        id         UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_school_id DEFAULT NEWID() PRIMARY KEY,
        user_id    UNIQUEIDENTIFIER NOT NULL,
        name       NVARCHAR(120)    NOT NULL,
        active     BIT              NOT NULL CONSTRAINT df_school_active DEFAULT 1,
        created_at DATETIME2(7)     NOT NULL CONSTRAINT df_school_created_at DEFAULT SYSUTCDATETIME(),
        updated_at DATETIME2(7)     NULL,
        -- hubspot_record_id se agrega en migración separada (P2 Fase 1.6).
        CONSTRAINT uk_school_user UNIQUE (user_id),
        CONSTRAINT fk_school_user FOREIGN KEY (user_id)
            REFERENCES users(id) ON DELETE CASCADE
    );
END;

IF OBJECT_ID('trg_school_updated_at', 'TR') IS NOT NULL
    DROP TRIGGER trg_school_updated_at;
EXEC('
CREATE TRIGGER trg_school_updated_at
ON school
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE s
       SET updated_at = SYSUTCDATETIME()
      FROM school s
     INNER JOIN inserted i ON s.id = i.id;
END
');

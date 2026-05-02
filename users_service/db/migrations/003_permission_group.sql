-- Tabla: permission_group. Grupos nombrados de permisos.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'permission_group')
BEGIN
    CREATE TABLE permission_group (
        id          INT            IDENTITY(1,1) NOT NULL PRIMARY KEY,
        code        NVARCHAR(80)   NOT NULL,
        name        NVARCHAR(120)  NOT NULL,
        description NVARCHAR(255)  NULL,
        active      BIT            NOT NULL CONSTRAINT df_permission_group_active DEFAULT 1,
        created_at  DATETIME2(7)   NOT NULL CONSTRAINT df_permission_group_created_at DEFAULT SYSUTCDATETIME(),
        updated_at  DATETIME2(7)   NULL,
        CONSTRAINT uk_permission_group_code UNIQUE (code)
    );
END;

IF OBJECT_ID('trg_permission_group_updated_at', 'TR') IS NOT NULL
    DROP TRIGGER trg_permission_group_updated_at;
EXEC('
CREATE TRIGGER trg_permission_group_updated_at
ON permission_group
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE g
       SET updated_at = SYSUTCDATETIME()
      FROM permission_group g
     INNER JOIN inserted i ON g.id = i.id;
END
');

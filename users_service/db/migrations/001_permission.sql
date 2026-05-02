-- T-SQL (Azure SQL Database)
-- Tabla: permission. Catálogo de permisos.
-- code = scope.action (ej: users.create, colegios.edit). scope = nombre del microservicio.
-- Trigger AFTER UPDATE mantiene updated_at sin que el adapter Go lo seteé.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'permission')
BEGIN
    CREATE TABLE permission (
        id          INT            IDENTITY(1,1) NOT NULL PRIMARY KEY,
        scope       NVARCHAR(80)   NOT NULL,
        code        NVARCHAR(80)   NOT NULL,
        name        NVARCHAR(120)  NOT NULL,
        description NVARCHAR(255)  NULL,
        active      BIT            NOT NULL CONSTRAINT df_permission_active DEFAULT 1,
        created_at  DATETIME2(7)   NOT NULL CONSTRAINT df_permission_created_at DEFAULT SYSUTCDATETIME(),
        updated_at  DATETIME2(7)   NULL,
        CONSTRAINT uk_permission_code UNIQUE (code)
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_permission_scope' AND object_id = OBJECT_ID('permission'))
    CREATE INDEX idx_permission_scope ON permission (scope);

-- CREATE TRIGGER debe ser el primer statement de su batch; lo envolvemos
-- en EXEC para poder agruparlo con el CREATE TABLE en una sola migración.
IF OBJECT_ID('trg_permission_updated_at', 'TR') IS NOT NULL
    DROP TRIGGER trg_permission_updated_at;
EXEC('
CREATE TRIGGER trg_permission_updated_at
ON permission
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    UPDATE p
       SET updated_at = SYSUTCDATETIME()
      FROM permission p
     INNER JOIN inserted i ON p.id = i.id;
END
');

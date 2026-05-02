-- Tabla: users (UNIQUEIDENTIFIER PK — referenciado cross-microservicio).
-- Sin FK a school todavía; la FK la agrega 007 (orden de dependencias).

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'users')
BEGIN
    CREATE TABLE users (
        id                  UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_users_id DEFAULT NEWID() PRIMARY KEY,
        email               NVARCHAR(255)    NOT NULL,
        password_hash       NVARCHAR(255)    NOT NULL,
        first_name          NVARCHAR(120)    NULL,
        last_name           NVARCHAR(120)    NULL,
        document_number     NVARCHAR(50)     NULL,
        school_id           UNIQUEIDENTIFIER NULL,
        active              BIT              NOT NULL CONSTRAINT df_users_active DEFAULT 1,
        last_access_at      DATETIME2(7)     NULL,
        created_at          DATETIME2(7)     NOT NULL CONSTRAINT df_users_created_at DEFAULT SYSUTCDATETIME(),
        updated_at          DATETIME2(7)     NULL,
        -- hubspot_record_id se agrega en migración separada (P2 Fase 1.6).
        CONSTRAINT uk_user_email           UNIQUE (email),
        CONSTRAINT uk_user_document_number UNIQUE (document_number)
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_user_active' AND object_id = OBJECT_ID('users'))
    CREATE INDEX idx_user_active ON users (active);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_user_school' AND object_id = OBJECT_ID('users'))
    CREATE INDEX idx_user_school ON users (school_id);

IF OBJECT_ID('trg_users_updated_at', 'TR') IS NOT NULL
    DROP TRIGGER trg_users_updated_at;
EXEC('
CREATE TRIGGER trg_users_updated_at
ON users
AFTER UPDATE
AS
BEGIN
    SET NOCOUNT ON;
    -- Solo si NO se actualizó updated_at directamente (evita loops).
    IF NOT UPDATE(updated_at)
        UPDATE u
           SET updated_at = SYSUTCDATETIME()
          FROM users u
         INNER JOIN inserted i ON u.id = i.id;
END
');

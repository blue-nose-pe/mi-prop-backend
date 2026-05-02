-- Refresh tokens persistidos server-side (permite revocación).
-- jti es el ID único del token (claim "jti" del JWT).
-- expires_at = iat + refresh_ttl (default 7 días).
-- Cuando se revoca (Logout) se setea revoked_at; el token sigue
-- siendo criptográficamente válido pero el server lo rechaza.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'refresh_token')
BEGIN
    CREATE TABLE refresh_token (
        jti           NVARCHAR(64)     NOT NULL PRIMARY KEY,
        user_id       UNIQUEIDENTIFIER NOT NULL,
        issued_at     DATETIME2(7)     NOT NULL CONSTRAINT df_refresh_token_issued_at DEFAULT SYSUTCDATETIME(),
        expires_at    DATETIME2(7)     NOT NULL,
        revoked_at    DATETIME2(7)     NULL,
        replaced_by   NVARCHAR(64)     NULL,                 -- jti del token que reemplazó a éste (rotación)
        ip            NVARCHAR(64)     NULL,
        user_agent    NVARCHAR(512)    NULL,
        CONSTRAINT fk_refresh_token_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_refresh_token_user' AND object_id = OBJECT_ID('refresh_token'))
    CREATE INDEX idx_refresh_token_user ON refresh_token (user_id, revoked_at);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_refresh_token_expires' AND object_id = OBJECT_ID('refresh_token'))
    CREATE INDEX idx_refresh_token_expires ON refresh_token (expires_at);

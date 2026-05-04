-- Tabla de OTPs para login de estudiante.
--
-- Flow:
--   1. POST /api/auth/student/request-otp { email } => server genera código
--      6 dígitos plaintext, lo hashea (HMAC-SHA256 con server-side secret),
--      persiste row con expires_at = now + 10 min y dispara webhook a HubSpot
--      que envía email con el plaintext.
--   2. POST /api/auth/student/verify-otp { email, otp } => server hashea el
--      input y compara con el hash persistido. Si match: marca consumed_at,
--      emite par access+refresh JWT.
--
-- Solo se permite UN OTP activo por usuario simultáneamente: cuando se pide
-- otro, los anteriores se invalidan automáticamente (consumed_at = now).
--
-- attempts cuenta intentos fallidos de verify; cuando llega a max_attempts (3)
-- el OTP se marca consumido aunque no se acertó (defensa contra brute-force).

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'otp_token')
BEGIN
    CREATE TABLE otp_token (
        id            UNIQUEIDENTIFIER NOT NULL PRIMARY KEY CONSTRAINT df_otp_token_id DEFAULT NEWID(),
        user_id       UNIQUEIDENTIFIER NOT NULL,
        code_hash     NVARCHAR(128)    NOT NULL,
        issued_at     DATETIME2(7)     NOT NULL CONSTRAINT df_otp_token_issued_at DEFAULT SYSUTCDATETIME(),
        expires_at    DATETIME2(7)     NOT NULL,
        consumed_at   DATETIME2(7)     NULL,
        attempts      INT              NOT NULL CONSTRAINT df_otp_token_attempts DEFAULT 0,
        max_attempts  INT              NOT NULL CONSTRAINT df_otp_token_max_attempts DEFAULT 3,
        ip            NVARCHAR(64)     NULL,
        CONSTRAINT fk_otp_token_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_otp_token_user_active' AND object_id = OBJECT_ID('otp_token'))
    CREATE INDEX idx_otp_token_user_active ON otp_token (user_id, consumed_at, expires_at);

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_otp_token_expires' AND object_id = OBJECT_ID('otp_token'))
    CREATE INDEX idx_otp_token_expires ON otp_token (expires_at);

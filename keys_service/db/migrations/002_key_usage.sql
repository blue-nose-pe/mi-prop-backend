-- key_usage: auditoría inmutable de cada uso (un attempt = un INSERT).
-- Sirve para reportería: "qué attempts entraron por la key X", "uso por
-- asesor", "intentos rechazados por aforo lleno".

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'key_usage')
BEGIN
    CREATE TABLE key_usage (
        id           UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_key_usage_id DEFAULT NEWID() PRIMARY KEY,
        key_id       UNIQUEIDENTIFIER NOT NULL,
        user_id      UNIQUEIDENTIFIER NOT NULL,
        attempt_id   UNIQUEIDENTIFIER NULL,                 -- ref lógica a exams_service.exam_attempt.id
        used_at      DATETIME2(7)     NOT NULL CONSTRAINT df_key_usage_used_at DEFAULT SYSUTCDATETIME(),
        CONSTRAINT fk_key_usage_key FOREIGN KEY (key_id) REFERENCES [key](id) ON DELETE CASCADE
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_key_usage_key' AND object_id = OBJECT_ID('key_usage'))
    CREATE INDEX idx_key_usage_key  ON key_usage (key_id, used_at);
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_key_usage_user' AND object_id = OBJECT_ID('key_usage'))
    CREATE INDEX idx_key_usage_user ON key_usage (user_id);

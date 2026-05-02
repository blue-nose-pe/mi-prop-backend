-- Audit log local del microservicio.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'audit_log')
BEGIN
    CREATE TABLE audit_log (
        id              UNIQUEIDENTIFIER NOT NULL CONSTRAINT df_audit_log_id DEFAULT NEWID() PRIMARY KEY,
        actor_user_id   UNIQUEIDENTIFIER NULL,
        action          NVARCHAR(255)    NOT NULL,
        entity_type     NVARCHAR(100)    NULL,
        entity_id       NVARCHAR(255)    NULL,
        payload_json    NVARCHAR(MAX)    NULL,
        correlation_id  NVARCHAR(64)     NULL,
        ip              NVARCHAR(64)     NULL,
        success         BIT              NOT NULL,
        error_message   NVARCHAR(MAX)    NULL,
        created_at      DATETIME2(7)     NOT NULL CONSTRAINT df_audit_log_created_at DEFAULT SYSUTCDATETIME()
    );
END;

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'idx_audit_log_actor' AND object_id = OBJECT_ID('audit_log'))
    CREATE INDEX idx_audit_log_actor ON audit_log (actor_user_id, created_at);

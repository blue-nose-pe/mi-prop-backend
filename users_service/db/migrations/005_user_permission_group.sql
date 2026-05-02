-- N:M users ↔ permission_group.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'user_permission_group')
BEGIN
    CREATE TABLE user_permission_group (
        user_id             UNIQUEIDENTIFIER NOT NULL,
        permission_group_id INT              NOT NULL,
        CONSTRAINT pk_user_permission_group
            PRIMARY KEY (user_id, permission_group_id),
        CONSTRAINT fk_upg_user
            FOREIGN KEY (user_id)             REFERENCES users(id)            ON DELETE CASCADE,
        CONSTRAINT fk_upg_group
            FOREIGN KEY (permission_group_id) REFERENCES permission_group(id) ON DELETE CASCADE
    );
END;

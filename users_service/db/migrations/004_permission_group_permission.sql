-- N:M permission_group ↔ permission.

IF NOT EXISTS (SELECT 1 FROM sys.tables WHERE name = 'permission_group_permission')
BEGIN
    CREATE TABLE permission_group_permission (
        permission_group_id INT NOT NULL,
        permission_id       INT NOT NULL,
        CONSTRAINT pk_permission_group_permission
            PRIMARY KEY (permission_group_id, permission_id),
        CONSTRAINT fk_pgp_group
            FOREIGN KEY (permission_group_id) REFERENCES permission_group(id) ON DELETE CASCADE,
        CONSTRAINT fk_pgp_permission
            FOREIGN KEY (permission_id)       REFERENCES permission(id)       ON DELETE CASCADE
    );
END;

-- Run connected to db_users.
-- Table: permission_group_permission (N:M group <-> permission)

CREATE TABLE IF NOT EXISTS permission_group_permission (
  permission_group_id INTEGER     NOT NULL,
  permission_id       INTEGER     NOT NULL,
  assigned_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (permission_group_id, permission_id),
  CONSTRAINT fk_pgp_group      FOREIGN KEY (permission_group_id)
    REFERENCES permission_group (id) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT fk_pgp_permission FOREIGN KEY (permission_id)
    REFERENCES permission (id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_pgp_permission ON permission_group_permission (permission_id);

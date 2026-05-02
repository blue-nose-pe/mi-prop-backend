-- Run connected to db_users.
-- Table: user_permission_group (N:M user <-> permission_group)
-- user_id is UUID; permission_group_id is INT.

CREATE TABLE IF NOT EXISTS user_permission_group (
  user_id             UUID        NOT NULL,
  permission_group_id INTEGER     NOT NULL,
  assigned_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (user_id, permission_group_id),
  CONSTRAINT fk_upg_user  FOREIGN KEY (user_id)
    REFERENCES users (id) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT fk_upg_group FOREIGN KEY (permission_group_id)
    REFERENCES permission_group (id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_upg_group ON user_permission_group (permission_group_id);

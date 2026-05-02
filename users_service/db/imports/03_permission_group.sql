-- Run connected to db_users.
-- Table: permission_group (named group of permissions, e.g. "student_permissions")

CREATE TABLE IF NOT EXISTS permission_group (
  id          INTEGER      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  code        VARCHAR(80)  NOT NULL,
  name        VARCHAR(120) NOT NULL,
  description VARCHAR(255) NULL,
  active      BOOLEAN      NOT NULL DEFAULT TRUE,
  created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ  NULL,
  CONSTRAINT uk_permission_group_code UNIQUE (code)
);

COMMENT ON COLUMN permission_group.code IS 'Unique identifier, e.g. student_permissions';

DROP TRIGGER IF EXISTS trg_permission_group_updated_at ON permission_group;
CREATE TRIGGER trg_permission_group_updated_at
BEFORE UPDATE ON permission_group
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

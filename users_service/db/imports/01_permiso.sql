-- Run connected to db_users.
-- Table: permission
-- code = scope.action (e.g. users.create, colegios.edit). Scope = microservice/DB name.

-- Shared trigger function to auto-update updated_at on row UPDATE.
-- Created once here; reused by every table that has updated_at.
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE IF NOT EXISTS permission (
  id          INTEGER      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  scope       VARCHAR(80)  NOT NULL,
  code        VARCHAR(80)  NOT NULL,
  name        VARCHAR(120) NOT NULL,
  description VARCHAR(255) NULL,
  active      BOOLEAN      NOT NULL DEFAULT TRUE,
  created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ  NULL,
  CONSTRAINT uk_permission_code UNIQUE (code)
);

COMMENT ON COLUMN permission.scope IS 'Microservice/DB name: users, colegios, etc.';
COMMENT ON COLUMN permission.code  IS 'Full identifier: scope.action (e.g. users.create)';

CREATE INDEX IF NOT EXISTS idx_permission_scope ON permission (scope);

DROP TRIGGER IF EXISTS trg_permission_updated_at ON permission;
CREATE TRIGGER trg_permission_updated_at
BEFORE UPDATE ON permission
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

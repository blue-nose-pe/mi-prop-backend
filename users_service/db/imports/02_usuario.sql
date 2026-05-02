-- Run connected to db_users.
-- Table: users (UUID primary key — id is referenced by other microservices)
-- Uses uuidv7() (PostgreSQL 18+): time-ordered UUIDs, excellent index locality.

CREATE TABLE IF NOT EXISTS users (
  id              UUID         NOT NULL DEFAULT uuidv7() PRIMARY KEY,
  email           VARCHAR(255) NOT NULL,
  password_hash   VARCHAR(255) NOT NULL,
  first_name      VARCHAR(120) NULL,
  last_name       VARCHAR(120) NULL,
  document_number VARCHAR(50)  NULL,
  school_id       UUID         NULL,
  active          BOOLEAN      NOT NULL DEFAULT TRUE,
  last_access_at  TIMESTAMPTZ  NULL,
  created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ  NULL,
  CONSTRAINT uk_user_email           UNIQUE (email),
  CONSTRAINT uk_user_document_number UNIQUE (document_number)
);

COMMENT ON COLUMN users.document_number IS 'ID document number (DNI, passport, etc.)';
COMMENT ON COLUMN users.school_id       IS 'Optional: school this user belongs to (school.id). NULL = not linked to a school';

CREATE INDEX IF NOT EXISTS idx_user_active ON users (active);
CREATE INDEX IF NOT EXISTS idx_user_school ON users (school_id);

DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
CREATE TRIGGER trg_users_updated_at
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

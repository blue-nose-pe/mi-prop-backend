-- Run connected to db_users.
-- Table: school (colegio); each school is represented by a user (user_id)

CREATE TABLE IF NOT EXISTS school (
  id         UUID         NOT NULL DEFAULT uuidv7() PRIMARY KEY,
  user_id    UUID         NOT NULL,
  name       VARCHAR(120) NOT NULL,
  active     BOOLEAN      NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ  NULL,
  CONSTRAINT uk_school_user UNIQUE (user_id),
  CONSTRAINT fk_school_user FOREIGN KEY (user_id)
    REFERENCES users (id) ON DELETE CASCADE ON UPDATE CASCADE
);

COMMENT ON COLUMN school.user_id IS 'User that represents this school (colegio as user)';

DROP TRIGGER IF EXISTS trg_school_updated_at ON school;
CREATE TRIGGER trg_school_updated_at
BEFORE UPDATE ON school
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

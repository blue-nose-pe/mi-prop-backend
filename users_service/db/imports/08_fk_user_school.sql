-- Run connected to db_users.
-- FK: users.school_id -> school.id (runs after both tables exist)

ALTER TABLE users
  DROP CONSTRAINT IF EXISTS fk_user_school;

ALTER TABLE users
  ADD CONSTRAINT fk_user_school FOREIGN KEY (school_id)
    REFERENCES school (id) ON DELETE SET NULL ON UPDATE CASCADE;

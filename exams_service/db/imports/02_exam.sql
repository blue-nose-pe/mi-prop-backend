-- Table: exam (uuid, time window, max participants; school_id optional: NULL = exam not tied to a school)
USE db_exams;

SET FOREIGN_KEY_CHECKS = 0;

CREATE TABLE exam (
  id                INT UNSIGNED NOT NULL AUTO_INCREMENT,
  uuid              CHAR(36)     NOT NULL COMMENT 'Public identifier',
  exam_type_id      INT UNSIGNED NOT NULL,
  school_id         INT UNSIGNED NULL COMMENT 'Optional: school that offers this exam (db_users.school.id). NULL = open exam, no school',
  name              VARCHAR(120) NOT NULL,
  start_at          DATETIME     NOT NULL,
  end_at            DATETIME     NOT NULL,
  max_participants  INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0 = unlimited',
  active            TINYINT(1)   NOT NULL DEFAULT 1,
  created_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at        DATETIME     NULL ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_exam_uuid (uuid),
  KEY idx_exam_type (exam_type_id),
  KEY idx_exam_school (school_id),
  KEY idx_exam_dates (start_at, end_at),
  CONSTRAINT fk_exam_type FOREIGN KEY (exam_type_id) REFERENCES exam_type (id) ON DELETE RESTRICT ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET FOREIGN_KEY_CHECKS = 1;

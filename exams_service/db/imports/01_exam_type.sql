-- Table: exam_type (type of exam: written, oral, practical, etc.)

USE db_exams;

CREATE TABLE exam_type (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  code        VARCHAR(50)  NOT NULL,
  name        VARCHAR(120) NOT NULL,
  active      TINYINT(1)   NOT NULL DEFAULT 1,
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME     NULL ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_exam_type_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

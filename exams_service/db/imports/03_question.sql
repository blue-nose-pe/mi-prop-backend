-- Table: question (question bank; all questions are multiple choice)

USE db_exams;

CREATE TABLE question (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  text        TEXT         NOT NULL,
  active      TINYINT(1)   NOT NULL DEFAULT 1,
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME     NULL ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

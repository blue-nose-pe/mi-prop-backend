-- Table: question_option (options for multiple_choice questions)

USE db_exams;

SET FOREIGN_KEY_CHECKS = 0;

CREATE TABLE question_option (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  question_id INT UNSIGNED NOT NULL,
  text        VARCHAR(500) NOT NULL,
  is_correct  TINYINT(1)   NOT NULL DEFAULT 0,
  sort_order  INT UNSIGNED NOT NULL DEFAULT 0,
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_question_option_question (question_id),
  CONSTRAINT fk_question_option_question FOREIGN KEY (question_id) REFERENCES question (id) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET FOREIGN_KEY_CHECKS = 1;

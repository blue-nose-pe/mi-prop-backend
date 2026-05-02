-- Table: exam_question (N:M exam <-> question; which questions are in each exam, and in what order)

USE db_exams;

SET FOREIGN_KEY_CHECKS = 0;

CREATE TABLE exam_question (
  exam_id     INT UNSIGNED NOT NULL,
  question_id INT UNSIGNED NOT NULL,
  points      INT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'Score for this question in this exam',
  sort_order  INT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (exam_id, question_id),
  KEY idx_exam_question_question (question_id),
  CONSTRAINT fk_exam_question_exam     FOREIGN KEY (exam_id)     REFERENCES exam (id) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT fk_exam_question_question FOREIGN KEY (question_id) REFERENCES question (id) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET FOREIGN_KEY_CHECKS = 1;

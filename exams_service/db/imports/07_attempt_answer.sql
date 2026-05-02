-- Table: attempt_answer (selected option per question; all questions are multiple choice)

USE db_exams;

SET FOREIGN_KEY_CHECKS = 0;

CREATE TABLE attempt_answer (
  exam_attempt_id    INT UNSIGNED NOT NULL,
  question_id        INT UNSIGNED NOT NULL,
  question_option_id INT UNSIGNED NOT NULL,
  PRIMARY KEY (exam_attempt_id, question_id),
  KEY idx_attempt_answer_question (question_id),
  CONSTRAINT fk_attempt_answer_attempt   FOREIGN KEY (exam_attempt_id) REFERENCES exam_attempt (id) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT fk_attempt_answer_question FOREIGN KEY (question_id) REFERENCES question (id) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT fk_attempt_answer_option   FOREIGN KEY (question_option_id) REFERENCES question_option (id) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET FOREIGN_KEY_CHECKS = 1;

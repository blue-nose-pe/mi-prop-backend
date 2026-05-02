-- Table: exam_attempt (who took the exam = user from db_users; works for students with/without school or any user)
USE db_exams;

SET FOREIGN_KEY_CHECKS = 0;

CREATE TABLE exam_attempt (
  id           INT UNSIGNED NOT NULL AUTO_INCREMENT,
  exam_id      INT UNSIGNED NOT NULL,
  user_id      INT UNSIGNED NOT NULL COMMENT 'User who took the exam (db_users.users.id)',
  started_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  submitted_at DATETIME     NULL,
  PRIMARY KEY (id),
  KEY idx_exam_attempt_exam (exam_id),
  KEY idx_exam_attempt_user (user_id),
  CONSTRAINT fk_exam_attempt_exam FOREIGN KEY (exam_id) REFERENCES exam (id) ON DELETE CASCADE ON UPDATE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET FOREIGN_KEY_CHECKS = 1;

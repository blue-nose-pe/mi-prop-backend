-- Seed: exam types

USE db_exams;

INSERT INTO exam_type (code, name) VALUES
  ('written',  'Written exam'),
  ('oral',     'Oral exam'),
  ('practical','Practical exam')
ON DUPLICATE KEY UPDATE name = VALUES(name);

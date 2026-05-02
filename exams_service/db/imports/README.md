# DB Exams (microservice)

MySQL 8.x. Multiple choice only. One simple rule: **whoever takes the exam is a user** (db_users). School and “student” are optional and live in db_users.

## Tables

| Table           | Description |
|-----------------|-------------|
| exam_type       | Type of exam (written, oral, practical) |
| exam            | **uuid**, exam_type_id, **school_id** (NULL = exam not tied to a school), name, **start_at**, **end_at**, **max_participants** |
| question        | Question bank |
| question_option | Options per question |
| exam_question   | Questions in exam + points + order |
| exam_attempt    | **user_id** (db_users) = who took the exam. No student_id. |
| attempt_answer  | Selected option per question |

## Logic (simple and flexible)

- **Who takes the exam:** always **user_id** (db_users). Same for students with school, students without school, or any user.
- **Exam and school:** **exam.school_id** NULL = exam not linked to any school (open exam). Set = school that offers it (db_users.school.id).
- **User and school:** In db_users, **user_school** (user_id, school_id); school_id NULL = independent. **school** = colegio (school.user_id = user that represents it).

Exam can be with or without school; the taker is always a user; if that user is also in db_users (with or without school_id), you have “student” info when you need it.

## Files (run in order)

| File | Description |
|------|-------------|
| 00_create_database.sql | Creates db_exams |
| 01_exam_type.sql      | Table exam_type |
| 02_exam.sql           | Table exam |
| 03_question.sql       | Table question |
| 04_question_option.sql| Table question_option |
| 05_exam_question.sql  | Table exam_question |
| 06_exam_attempt.sql   | Table exam_attempt (user_id only) |
| 07_attempt_answer.sql | Table attempt_answer |
| 08_seed_exam_types.sql| Example exam types |

## Run

`.\ejecutar_todo.ps1 -Usuario root`

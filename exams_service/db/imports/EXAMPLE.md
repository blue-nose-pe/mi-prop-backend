# Guía completa: cómo funciona todo

Dos bases de datos: **db_users** (usuarios, permisos, colegios) y **db_exams** (exámenes, preguntas, respuestas).

---

## db_users: tablas y para qué sirven

| Tabla | Para qué |
|-------|----------|
| **users** | Usuarios. Tiene **school_id** (NULL = sin colegio). |
| **school** | Colegio. Un colegio ES un usuario: **school.user_id** = el user que representa al colegio. |
| **permission** | Permisos (scope.action, ej: users.create, colegios.edit). |
| **permission_group** | Grupo de permisos (ej: "permisos_estudiantes"). |
| **permission_group_permission** | Qué permisos tiene cada grupo. |
| **user_permission_group** | Qué grupos tiene cada usuario. |

---

## db_exams: tablas y para qué sirven

| Tabla | Para qué |
|-------|----------|
| **exam_type** | Tipos de examen (written, oral, practical). |
| **exam** | Examen: uuid, tipo, school_id (opcional), nombre, start_at, end_at, max_participants. |
| **question** | Banco de preguntas (solo texto). |
| **question_option** | Opciones de cada pregunta. **is_correct = 1** marca la respuesta correcta. |
| **exam_question** | Qué preguntas tiene un examen, en qué orden y cuántos puntos vale cada una. |
| **exam_attempt** | Un usuario rindió un examen (user_id, started_at, submitted_at). |
| **attempt_answer** | La opción que eligió el usuario por cada pregunta. |

---

## Ejemplo completo paso a paso

### Paso 1: Crear colegio y usuarios (db_users)

```sql
USE db_users;

-- Usuario que representa al Colegio ABC
INSERT INTO users (email, password_hash, first_name) VALUES
  ('admin@colegioabc.edu', 'hash123', 'Colegio ABC');
-- school.user_id = 1 (ese usuario)
INSERT INTO school (user_id, name) VALUES (1, 'Colegio ABC');

-- María, alumna del Colegio ABC (school_id = 1)
INSERT INTO users (email, password_hash, first_name, last_name, school_id) VALUES
  ('maria@mail.com', 'hash456', 'María', 'García', 1);

-- Pedro, usuario independiente (school_id = NULL, sin colegio)
INSERT INTO users (email, password_hash, first_name, last_name, school_id) VALUES
  ('pedro@mail.com', 'hash789', 'Pedro', 'López', NULL);
```

Resultado:

| users.id | email | first_name | school_id |
|----------|-------|------------|-----------|
| 1 | admin@colegioabc.edu | Colegio ABC | NULL |
| 2 | maria@mail.com | María | 1 (Colegio ABC) |
| 3 | pedro@mail.com | Pedro | NULL (sin colegio) |

| school.id | user_id | name |
|-----------|---------|------|
| 1 | 1 | Colegio ABC |

---

### Paso 2: Crear preguntas con opciones y respuesta correcta (db_exams)

```sql
USE db_exams;

INSERT INTO question (text) VALUES
  ('What is 2 + 2?'),
  ('What is the capital of France?');
```

| question.id | text |
|-------------|------|
| 1 | What is 2 + 2? |
| 2 | What is the capital of France? |

```sql
INSERT INTO question_option (question_id, text, is_correct, sort_order) VALUES
  (1, '3', 0, 0),
  (1, '4', 1, 1),   -- ← CORRECTA
  (1, '5', 0, 2),
  (2, 'London', 0, 0),
  (2, 'Paris',  1, 1),  -- ← CORRECTA
  (2, 'Berlin', 0, 2);
```

| option.id | question_id | text | is_correct | sort_order |
|-----------|-------------|------|------------|------------|
| 1 | 1 | 3 | 0 | 0 |
| 2 | 1 | **4** | **1** | 1 |
| 3 | 1 | 5 | 0 | 2 |
| 4 | 2 | London | 0 | 0 |
| 5 | 2 | **Paris** | **1** | 1 |
| 6 | 2 | Berlin | 0 | 2 |

**is_correct = 1** es la respuesta correcta. Así sabés cuál es.

---

### Paso 3: Crear examen y asignar preguntas con puntos (db_exams)

```sql
INSERT INTO exam_type (code, name) VALUES ('written', 'Written exam');

INSERT INTO exam (uuid, exam_type_id, school_id, name, start_at, end_at, max_participants) VALUES
  (UUID(), 1, 1, 'Math midterm 2025', '2025-03-01 08:00:00', '2025-03-01 18:00:00', 50);
```

| exam.id | uuid | school_id | name | start_at | end_at | max_participants |
|---------|------|-----------|------|----------|--------|------------------|
| 1 | abc-... | 1 (Colegio ABC) | Math midterm 2025 | 2025-03-01 08:00 | 2025-03-01 18:00 | 50 |

```sql
INSERT INTO exam_question (exam_id, question_id, points, sort_order) VALUES
  (1, 1, 10, 0),
  (1, 2, 10, 1);
```

| exam_id | question_id | points | sort_order |
|---------|-------------|--------|------------|
| 1 | 1 (2+2?) | 10 | 0 |
| 1 | 2 (capital?) | 10 | 1 |

Total: 20 puntos.

---

### Paso 4: María rinde el examen y responde

```sql
INSERT INTO exam_attempt (exam_id, user_id) VALUES (1, 2);

INSERT INTO attempt_answer (exam_attempt_id, question_id, question_option_id) VALUES
  (1, 1, 2),   -- eligió opción 2 ("4") → correcta
  (1, 2, 5);   -- eligió opción 5 ("Paris") → correcta
```

| exam_attempt.id | exam_id | user_id |
|-----------------|---------|---------|
| 1 | 1 | 2 (María) |

| exam_attempt_id | question_id | question_option_id |
|-----------------|-------------|--------------------|
| 1 | 1 | 2 ("4") |
| 1 | 2 | 5 ("Paris") |

---

### Paso 5: ¿Cómo saber si respondió bien?

Comparás la opción que eligió con **is_correct**:

```sql
SELECT
  q.text                  AS pregunta,
  chosen.text             AS respuesta_elegida,
  chosen.is_correct       AS acerto
FROM attempt_answer aa
JOIN question q          ON q.id = aa.question_id
JOIN question_option chosen ON chosen.id = aa.question_option_id
WHERE aa.exam_attempt_id = 1;
```

Resultado:

| pregunta | respuesta_elegida | acerto |
|----------|-------------------|--------|
| What is 2 + 2? | 4 | 1 (sí) |
| What is the capital of France? | Paris | 1 (sí) |

Para calcular el puntaje total:

```sql
SELECT SUM(eq.points) AS score
FROM attempt_answer aa
JOIN question_option chosen ON chosen.id = aa.question_option_id
JOIN exam_question eq       ON eq.exam_id = aa.exam_attempt_id
                            AND eq.question_id = aa.question_id
WHERE aa.exam_attempt_id = 1
  AND chosen.is_correct = 1;
```

Resultado: **score = 20** (10 + 10, respondió ambas bien).

---

## Flujo resumido

```
db_users:
  users (school_id opcional)
    ↔ school (colegio = un usuario)
    ↔ user_permission_group → permission_group_permission → permission

db_exams:
  exam_type → exam (uuid, school_id, start_at, end_at, max_participants)
                ↓
              exam_question (points, sort_order)
                ↓
              question → question_option (is_correct = respuesta correcta)

  exam_attempt (user_id = quien rindió)
    ↓
  attempt_answer (question_option_id = opción elegida)

  ¿Acertó? → attempt_answer.question_option_id → question_option.is_correct = 1
```

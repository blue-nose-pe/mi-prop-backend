# Guía simple: base de datos `db_users`

PostgreSQL 18. Esta guía explica **de qué sirve cada tabla**, **cómo se relacionan**, y **cómo se usan** con ejemplos reales que puedes copiar en pgAdmin o psql y ejecutar.

---

## 🧠 Idea general (3 conceptos)

1. **Todo usuario es un `user`** — incluso un colegio. Un colegio no tiene su propia tabla de login; es un usuario con un rol especial.
2. **Los colegios agrupan usuarios** — un alumno tiene `users.school_id` apuntando al colegio al que pertenece. Si es NULL, no pertenece a ninguno.
3. **Los permisos se dan por grupos, no uno por uno** — en lugar de decir "este usuario puede ver, crear y editar", creas un grupo `student_permissions` con esos 3 permisos adentro, y se lo asignas al usuario. Si mañana cambia qué puede hacer un alumno, tocas el grupo una vez y se propaga a todos.

```
      ┌───────────────────┐         ┌─────────────┐
      │      users        │─school──│   school    │
      │  (personas+colegi)│  _id    │  (colegio)  │
      └─────────┬─────────┘         └─────────────┘
                │ user.id
                ▼
      ┌──────────────────────┐
      │ user_permission_group│  ← "maría tiene estos grupos"
      └──────────┬───────────┘
                 │ permission_group_id
                 ▼
      ┌──────────────────────┐
      │   permission_group   │  ← "el grupo student_permissions"
      └──────────┬───────────┘
                 │ id
                 ▼
      ┌────────────────────────────────┐
      │ permission_group_permission    │  ← "este grupo trae estos permisos"
      └──────────────┬─────────────────┘
                     │ permission_id
                     ▼
      ┌──────────────────────────────────┐
      │           permission             │  ← "users.view, users.create, ..."
      └──────────────────────────────────┘
```

---

## 📋 Las 6 tablas, una por una

| Tabla | Qué guarda | Ejemplo |
|-------|-----------|---------|
| **users** | Personas que pueden loguearse. Los colegios también viven aquí. | María, Pedro, "Colegio ABC" |
| **school** | Lista de colegios. Cada colegio apunta al `users.id` que lo representa. | Colegio ABC |
| **permission** | Catálogo de acciones. Cada fila = una acción concreta. | `users.view`, `colegios.edit` |
| **permission_group** | Paquetes con nombre (como "roles" reutilizables). | `student_permissions` |
| **permission_group_permission** | Qué permisos trae cada grupo (tabla intermedia). | `student_permissions` → `users.view` + `users.create` + `colegios.view` |
| **user_permission_group** | Qué grupos tiene cada usuario (tabla intermedia). | María → `student_permissions` |

### Por qué hay dos tablas "intermedia" (las que unen)

Las relaciones **N a M** (un grupo puede tener muchos permisos; un permiso puede estar en muchos grupos) no caben en una sola tabla — necesitas una "tabla puente". Lo mismo con usuarios y grupos: un usuario puede estar en muchos grupos, un grupo puede tener muchos usuarios.

---

## 🎬 Historia paso a paso

Vamos a construir desde cero un mini ejemplo: **Colegio ABC** con dos alumnos (María y Juan) y un usuario independiente (Pedro). Todos los scripts son para ejecutar en `db_users`.

### Paso 1 — Crear el usuario que representa al colegio

```sql
INSERT INTO users (email, password_hash, first_name)
VALUES ('admin@colegioabc.edu', 'hash_de_la_pass', 'Colegio ABC');
```

Esto crea UN usuario con `first_name = 'Colegio ABC'`. Todavía **no es** un colegio — solo un usuario más. El `id` se genera solo (UUID v7).

### Paso 2 — Registrar el colegio y vincularlo al usuario

```sql
INSERT INTO school (user_id, name)
SELECT id, 'Colegio ABC'
  FROM users
 WHERE email = 'admin@colegioabc.edu';
```

Usamos `SELECT` para traer el `id` del usuario que acabamos de crear (así no hay que copiar el UUID a mano). Ahora `school.user_id = users.id`, o sea: **ese usuario ES el colegio**.

### Paso 3 — Crear alumnos del colegio

```sql
-- María, alumna del Colegio ABC
INSERT INTO users (email, password_hash, first_name, last_name, school_id)
SELECT 'maria@mail.com', 'hash', 'María', 'García', id
  FROM school WHERE name = 'Colegio ABC';

-- Juan, también del Colegio ABC
INSERT INTO users (email, password_hash, first_name, last_name, school_id)
SELECT 'juan@mail.com', 'hash', 'Juan', 'Pérez', id
  FROM school WHERE name = 'Colegio ABC';
```

Nota la diferencia: **Colegio ABC** tiene `school_id = NULL` (porque él **es** el colegio, no pertenece a uno). María y Juan tienen `school_id` apuntando a Colegio ABC.

### Paso 4 — Crear un usuario sin colegio

```sql
INSERT INTO users (email, password_hash, first_name, last_name)
VALUES ('pedro@mail.com', 'hash', 'Pedro', 'López');
```

`school_id` se queda NULL porque no lo pasamos. Pedro no pertenece a ningún colegio. Perfectamente válido.

### Estado actual de `users`

| email | first_name | school_id |
|---|---|---|
| admin@colegioabc.edu | Colegio ABC | NULL ← él es el colegio |
| maria@mail.com | María | (uuid del colegio) |
| juan@mail.com | Juan | (uuid del colegio) |
| pedro@mail.com | Pedro | NULL ← solo |

### Paso 5 — Un grupo de permisos ya existe: `student_permissions`

El seed (06_seed_permisos.sql) ya creó el grupo `student_permissions` con 3 permisos: `users.view`, `users.create`, `colegios.view`. No hace falta crearlo de nuevo. Para confirmar:

```sql
SELECT pg.code AS grupo, p.code AS permiso
  FROM permission_group pg
  JOIN permission_group_permission pgp ON pgp.permission_group_id = pg.id
  JOIN permission p                    ON p.id = pgp.permission_id
 WHERE pg.code = 'student_permissions';
```

Resultado:
```
        grupo        |    permiso
---------------------+----------------
 student_permissions | users.view
 student_permissions | users.create
 student_permissions | colegios.view
```

### Paso 6 — Crear un grupo NUEVO (por si quieres inventar otro)

```sql
-- 1. Crear el grupo (vacío)
INSERT INTO permission_group (code, name, description)
VALUES ('teacher_permissions', 'Permisos de profesor', 'Ver y editar usuarios');

-- 2. Llenar el grupo con permisos ya existentes (busca por code, no por id)
INSERT INTO permission_group_permission (permission_group_id, permission_id)
SELECT g.id, p.id
  FROM permission_group g
 CROSS JOIN permission p
 WHERE g.code = 'teacher_permissions'
   AND p.code IN ('users.view', 'users.edit', 'colegios.view');
```

**Regla importante**: no escribas IDs a mano. Siempre busca por `code` (que es legible) y deja que la DB encuentre el `id`. Así tu script funciona aunque los IDs cambien.

### Paso 7 — Asignar un grupo a un usuario

```sql
-- María tiene student_permissions
INSERT INTO user_permission_group (user_id, permission_group_id)
SELECT u.id, g.id
  FROM users u, permission_group g
 WHERE u.email = 'maria@mail.com'
   AND g.code = 'student_permissions';
```

Listo. María ahora tiene 3 permisos (los que trae el grupo): `users.view`, `users.create`, `colegios.view`.

### Paso 8 — Asignarle DOS grupos al mismo usuario

```sql
-- Juan tiene student_permissions + teacher_permissions
INSERT INTO user_permission_group (user_id, permission_group_id)
SELECT u.id, g.id
  FROM users u, permission_group g
 WHERE u.email = 'juan@mail.com'
   AND g.code IN ('student_permissions', 'teacher_permissions');
```

Juan acumula los permisos de ambos grupos. Si los grupos tienen permisos repetidos, no pasa nada — el `DISTINCT` en las consultas los une.

---

## 🔍 Consultas típicas del día a día

### ¿Qué permisos tiene María?

```sql
SELECT DISTINCT p.code
  FROM user_permission_group upg
  JOIN permission_group_permission pgp ON pgp.permission_group_id = upg.permission_group_id
  JOIN permission p                    ON p.id = pgp.permission_id
 WHERE upg.user_id = (SELECT id FROM users WHERE email = 'maria@mail.com');
```

```
     code
----------------
 colegios.view
 users.create
 users.view
```

### ¿Puede María editar usuarios? (o sea, ¿tiene `users.edit`?)

```sql
SELECT EXISTS (
  SELECT 1
    FROM user_permission_group upg
    JOIN permission_group_permission pgp ON pgp.permission_group_id = upg.permission_group_id
    JOIN permission p                    ON p.id = pgp.permission_id
   WHERE upg.user_id = (SELECT id FROM users WHERE email = 'maria@mail.com')
     AND p.code = 'users.edit'
) AS puede;
```

Resultado: `f` (false — no lo tiene en ninguno de sus grupos).

### ¿Qué alumnos tiene el Colegio ABC?

```sql
SELECT u.first_name, u.last_name, u.email
  FROM users u
  JOIN school s ON s.id = u.school_id
 WHERE s.name = 'Colegio ABC';
```

```
 first_name | last_name |     email
------------+-----------+----------------
 María      | García    | maria@mail.com
 Juan       | Pérez     | juan@mail.com
```

### ¿Cuántos alumnos tiene cada colegio?

```sql
SELECT s.name, COUNT(u.id) AS alumnos
  FROM school s
  LEFT JOIN users u ON u.school_id = s.id
 GROUP BY s.name;
```

### ¿Qué usuarios pertenecen a un grupo?

```sql
SELECT u.email
  FROM users u
  JOIN user_permission_group upg ON upg.user_id = u.id
  JOIN permission_group pg       ON pg.id = upg.permission_group_id
 WHERE pg.code = 'student_permissions';
```

### Quitar un grupo a un usuario

```sql
DELETE FROM user_permission_group
 WHERE user_id = (SELECT id FROM users WHERE email = 'juan@mail.com')
   AND permission_group_id = (SELECT id FROM permission_group WHERE code = 'teacher_permissions');
```

### Desactivar un usuario (sin borrarlo)

```sql
UPDATE users SET active = FALSE
 WHERE email = 'pedro@mail.com';
```

El `active = FALSE` es la forma recomendada — borrar deja huecos (ids rotos en otras tablas). En login, tu app debe rechazar usuarios con `active = FALSE`.

---

## 📐 Reglas importantes

| Regla | Por qué |
|---|---|
| **No escribas IDs a mano** | Los UUIDs son largos y las identidades cambian entre entornos. Siempre busca por `email` / `code`. |
| **Un colegio ES un usuario** (`school.user_id` obligatorio) | El colegio puede loguearse como un usuario más (con su email admin). Tiene `school.id` propio solo para que otros usuarios apunten a él. |
| **Un usuario puede o no pertenecer a un colegio** (`users.school_id` opcional, NULL = sin colegio) | Los colegios-usuario no pertenecen a nadie; los alumnos sí pertenecen a uno. |
| **Los permisos NO se asignan directo al usuario** | Siempre van por grupo. Si quieres darle solo un permiso, crea un grupo de 1 permiso. Esto mantiene todo uniforme. |
| **Borrar un usuario CASCADE** | Borrar el usuario borra sus filas en `user_permission_group` y en `school` (si era un colegio). Pero `users.school_id` solo se pone en NULL (no se borra el alumno si borras su colegio). |
| **El `code` es la clave real** | `permission.code` y `permission_group.code` son únicos y legibles. El `id` entero existe solo para JOINs rápidos. |

---

## 🧹 Limpiar todo (volver al estado del seed)

```sql
TRUNCATE user_permission_group, permission_group_permission RESTART IDENTITY CASCADE;
-- school y users no se tocan para no perder tu Colegio ABC + alumnos
-- si quieres empezar 100% de cero:
TRUNCATE school, users RESTART IDENTITY CASCADE;
```

O usar el script: `.\ejecutar_todo.ps1 -Usuario postgres -Password "0510" -Recreate`

---

## 🔗 Resumen del flujo de permisos

```
usuario (maría)
   │
   │  user_permission_group
   ▼
grupo (student_permissions)
   │
   │  permission_group_permission
   ▼
permisos (users.view, users.create, colegios.view)
```

Tres tablas para contestar **"¿qué puede hacer este usuario?"** — pero eso te permite cambiar el comportamiento de miles de usuarios modificando una sola fila (el grupo).

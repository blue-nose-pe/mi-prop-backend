# DB Users (microservice)

**PostgreSQL 18+** (needs native `uuidv7()`). **Users**, **permission groups**, and **school (colegio)**. A school is represented by a user; users have an optional **school_id** (NULL = not linked to any school).

## Primary key strategy

| Table | PK | Why |
|-------|----|----|
| **users** | `UUID` (uuidv7) | Referenced by other microservices; never expose sequential IDs cross-service |
| **school** | `UUID` (uuidv7) | Same reason as users — crosses service boundaries |
| **permission** | `INTEGER` identity | Internal config table; natural key is `code` (e.g. `users.create`) |
| **permission_group** | `INTEGER` identity | Same as permission: internal, small, seeded by code |
| join tables | composite PKs | No surrogate id needed |

`uuidv7()` gives **time-ordered** UUIDs — far better index locality than v4.

## Tables

| Table | Description |
|-------|-------------|
| permission | Permissions (scope + code, e.g. users.create, colegios.edit) |
| users | Users; **school_id** NULL = not linked to a school |
| permission_group | Named group of permissions |
| permission_group_permission | N:M group ↔ permission |
| user_permission_group | N:M user ↔ permission_group |
| school | School (colegio); **user_id** = the user that represents this school |

**User permissions:** user → user_permission_group → permission_group_permission → permission.

**School:** One user can "be" a school (school.user_id). Any user can belong to a school via **users.school_id**.

## Files (run in order)

| File | Description |
|------|-------------|
| 00_create_database.sql | Creates db_users (connect to `postgres` to run it) |
| 01_permiso.sql | Creates `set_updated_at()` trigger function + table `permission` |
| 02_usuario.sql | Table `users` (UUID PK, default `uuidv7()`) |
| 03_permission_group.sql | Table `permission_group` |
| 04_permission_group_permission.sql | Group ↔ permission (INT + INT) |
| 05_user_permission_group.sql | User ↔ group (UUID + INT) |
| 06_seed_permisos.sql | Permissions (`users.*`, `permission.*`, `colegios.*`) + example group |
| 07_school.sql | Table `school` (UUID PK) |
| 08_fk_user_school.sql | FK users.school_id → school.id |

## Run

```powershell
# Postgres must be running (service postgresql-x64-18).
.\ejecutar_todo.ps1 -Usuario postgres -Password "tu_pass"

# Recrear desde cero (DROP DATABASE + create):
.\ejecutar_todo.ps1 -Usuario postgres -Password "tu_pass" -Recreate
```

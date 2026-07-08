# Modelo de permisos — Mi Propósito 2.0

Documento de referencia del sistema de autorización del backend. Describe **lo que el código hace hoy**: catálogo de permisos, grupos, cómo el gateway gatea cada endpoint, el scope por colegio y los casos especiales (LAN masiva, coordinadores).

Fuentes verificadas:
- Esquema y seed: `users_service/db/migrations/001..039_*.sql`.
- Enforcement del gateway: `gateway/internal/proxy/helpers.go`, `keys.go`, `users.go`, `auth.go`.
- Claims del token: `gateway/internal/middleware/jwt.go`.
- BD real (`db_users`, tablas `permission`, `permission_group`, `permission_group_permission`) consultada en vivo.

---

## 1. Cómo funciona

### 1.1 Modelo de datos

El modelo es un RBAC clásico de tres tablas (más el puente usuario↔grupo):

| Tabla | Migración | Rol |
| --- | --- | --- |
| `permission` | `001_permission.sql` | Catálogo de permisos. Cada fila tiene `scope`, `code` (único), `name`, `description`, `active`. |
| `permission_group` | `003_permission_group.sql` | Grupos nombrados (roles). `code` único, `name`, `description`, `active`. |
| `permission_group_permission` | `004_permission_group_permission.sql` | N:M grupo ↔ permiso (PK compuesta, `ON DELETE CASCADE`). |
| `user_permission_group` | `005_user_permission_group.sql` | N:M usuario ↔ grupo (un usuario puede tener varios grupos; son **aditivos**). |

Un usuario **no** recibe permisos sueltos: recibe **grupos**, y el conjunto efectivo de permisos es la unión de los permisos de todos sus grupos.

### 1.2 Convención de los `code`

El formato canónico del `code` es `scope.recurso.accion` (reseed en `014_reseed_permissions.sql`):

- **scope** = microservicio / BD destino: `db_users`, `db_exams`, `db_keys`, `db_satisfaction`, `hubspot`, `analytics`.
- **recurso** = tabla o área lógica (`users`, `school`, `key`, `survey`, `dashboard`, `exam_attempt`, `lan`, `coordinador`, …).
- **accion** = `read` | `write` (la granularidad puede crecer sin romper nada).

Ejemplos reales: `db_users.users.read`, `db_keys.key.write`, `analytics.dashboard.read`, `db_satisfaction.survey.write`.

> El seed original (`008_seed_permissions.sql`) usaba el formato viejo `users.view`, `colegios.edit`, etc. La migración 014 **elimina** ese catálogo y lo reemplaza por el formato `scope.recurso.accion`. Los codes viejos ya no existen en BD ni los chequea el código.

### 1.3 El JWT lleva los permisos

En el login (`AuthService.Login`), el users-service agrega los permisos de todos los grupos del usuario y los devuelve. El gateway los embebe en el access token junto con los roles y (si aplica) el `school_id` — ver `auth.go` (`perms := resp.GetPermissions()` → `Permissions: perms`). Los claims que viajan en el token están definidos en `gateway/internal/middleware/jwt.go`:

```go
Roles       []string `json:"roles,omitempty"`
Permissions []string `json:"permissions,omitempty"`
SchoolID    string   `json:"school_id,omitempty"`
```

Consecuencia operativa clave: **la autorización se resuelve leyendo el token, no la BD**. El gateway no consulta `permission_group_permission` en cada request; confía en la lista `permissions` del JWT (sí hace RPCs al users-service para resolver el *scope por colegio*, ver §4).

### 1.4 El gate canónico: `hasPermission`

Todo endpoint protegido se gatea con `hasPermission(r, "<code>")` (`gateway/internal/proxy/helpers.go`):

```go
func hasPermission(r *http.Request, code string) bool {
    if isSuperadminContext(r) { return true }   // superadmin bypassa
    c := middleware.ClaimsFromContext(r.Context())
    if c == nil { return false }
    for _, p := range c.Permissions {
        if p == code { return true }
    }
    return false
}
```

Reglas de diseño (documentadas en el propio helper):

- **Superadmin bypassa todo.** Un usuario con rol `superadmin` (`users.is_superadmin = 1`) siempre pasa `hasPermission`, sin importar sus grupos. El superadmin es un atajo operacional, no un permiso especial.
- **Nada es exclusivo del superadmin por diseño.** La filosofía es: si UCSP quiere que un asesor de confianza vea el comparativo global, se crea/asigna un grupo con `analytics.dashboard.read`. El superadmin solo evita tener que armar la matriz al primer install.
- **Cerrado por defecto.** Sin el permiso (y sin ser superadmin), el gate deniega con `403 PERMISSION_DENIED`.

### 1.5 El "admin-marker"

Hay un segundo marcador, más fuerte que un permiso suelto: **quién puede administrar cualquier usuario / ver toda la plataforma**. Se detecta con `db_users.permission_group.write`, que en BD **solo** tiene `admin_permissions` (y superadmin lo bypassa):

```go
func callerIsUserAdmin(r *http.Request) bool {
    return isSuperadminContext(r) || hasPermission(r, "db_users.permission_group.write")
}
```

Este marcador se usa en `enforceColegioScope`, `enforceAsesorScope`, `enforceUserScope` y `callerColegioScope` para que un **admin no-superadmin** vea TODOS los colegios / usuarios (no queda atado a un colegio como el asesor). Sin él, un admin sin `is_superadmin` recibía 403 en todo endpoint scopeado por colegio.

Corolario de seguridad (audit 2026-07-02): un `coordinador` tiene `db_users.users.write` para gestionar **estudiantes**, pero NO `db_users.permission_group.write`, así que **no es admin** y no puede fabricar/editar cuentas de staff (asesores, coordinadores, admins) — se evita escalada de privilegios.

---

## 2. Grupos de permisos (roles) — tabla real de BD

Consulta en vivo de `permission_group` (`db_users`). Los grupos operacionales son los primeros; el resto son grupos de **test/descartables** creados por workflows E2E y de auditoría.

| id | code | name | Para qué es |
| --- | --- | --- | --- |
| 1 | `student_permissions` | Permissions for students | **Estudiante/alumno.** Grupo por defecto del auto-registro con key pública. Rinde exámenes y responde encuestas. |
| 3 | `asesor_permissions` | Asesor comercial | **Asesor.** Lectura amplia + escribe llaves y (hoy) exámenes; ve dashboards y portal de **sus** colegios. NO crea colegios. |
| 4 | `coordinador_permissions` | Coordinador colegio | **Coordinador de colegio.** Vista acotada a su(s) colegio(s); gestiona **estudiantes** (crear/actualizar); NO crea llaves. |
| 6 | `admin_permissions` | Admin UCSP | **Admin general UCSP.** CRUD completo sobre todas las tablas operacionales. Es el "admin-marker" (ve toda la plataforma). |
| 15 | `marketing_permissions` | Marketing | **Marketing.** Ve el apartado "Simulacro Masivo" + dashboards/exports de campaña. |
| 16 | `colegio_permissions` | Colegio | **Cuenta del colegio cliente.** Solo el "Portal del Colegio" de su propio colegio (dashboard + descarga de informe). |
| 20 | `coordinador_management` | Gestión de coordinadores | **Grupo dedicado** que otorga `db_users.coordinador.write`. El superadmin lo asigna caso por caso a asesores (ver §6). |
| 7 | `test_reader` | Test Reader | Grupo de test (descartable). |
| 8 | `test_writer` | Test Writer | Grupo de test (descartable). |
| 9 | `all_perms_test` | All permissions (descartable test) | Grupo de test con todos los permisos. |
| 17 | `permtest_full` | PermTest Full | Grupo de auditoría de permisos (descartable). |
| 18 | `asesor_school_write` | Asesor con edición de colegio | Grupo de test (asesor + `school.write`). |
| 19 | `e2e_grupo_1782967060148` | E2E Grupo 1782967060148 | Grupo generado por workflow E2E (descartable). |
| 21 | `ph_roizc6` | permhunt-esc-group-ct54z2 | Grupo generado por caza-bugs de escalada (descartable). |

> Nota: los grupos de test (7, 8, 9, 17, 18, 19, 21) no son parte del modelo de producto; conviven en la BD demo por la regla de no borrar data de prueba. La matriz canónica de producto es: student / asesor / coordinador / admin / marketing / colegio / coordinador_management.

---

## 3. Matriz real grupo → permisos (BD en vivo)

Salida directa de `permission_group_permission` para los grupos operacionales.

### 3.1 `admin_permissions` (Admin UCSP)

Tiene **todo el catálogo**, incluido `db_users.permission.write` y `db_users.permission_group.write` (el admin-marker), `db_keys.lan.read`/`.write` y todo `hubspot.*`. Es el único grupo (además de superadmin) que puede gestionar el catálogo de permisos y grupos a runtime.

### 3.2 `asesor_permissions` (Asesor comercial)

| Permiso | |
| --- | --- |
| `analytics.colegio_portal.read` | Portal del Colegio (solo sus colegios, migr 027) |
| `analytics.dashboard.read` | Dashboards agregados |
| `analytics.export.write` | Exports XLSX |
| `db_exams.exam.read` / `db_exams.exam.write` | Ver y crear/editar exámenes |
| `db_exams.exam_attempt.read` | Ver intentos (no `.write`: no rinde exámenes) |
| `db_exams.exam_type.read` | Catálogo de tipos de examen |
| `db_keys.key.read` / `db_keys.key.write` / `db_keys.key_usage.read` | Llaves de colegio (generar/editar) + auditoría de uso |
| `db_satisfaction.survey.read` / `db_satisfaction.survey_response.read` | Ver encuestas y respuestas |
| `db_users.assignment.read` | Ver asignaciones asesor/colegio |
| `db_users.school.read` | Ver colegios (**NO** `school.write`: no crea colegios — migr 025) |
| `db_users.users.read` | Ver usuarios |
| `hubspot.contact.read` | Lookup de contactos |

No tiene: `db_users.school.write`, `db_users.permission_group.write` (no es admin), `db_keys.lan.*` (no gestiona LAN por defecto), `db_users.coordinador.write` (se retiró del grupo base en migr 038; se otorga vía `coordinador_management`).

### 3.3 `coordinador_permissions` (Coordinador colegio)

| Permiso | |
| --- | --- |
| `analytics.colegio_portal.read` | Portal del Colegio de su colegio (migr 032) |
| `analytics.dashboard.read` | Dashboards |
| `db_exams.exam.read` / `db_exams.exam_attempt.read` / `db_exams.exam_type.read` | Lectura de exámenes e intentos |
| `db_keys.key.read` | Ver llaves (**NO** `key.write`: no crea llaves) |
| `db_satisfaction.survey.read` / `db_satisfaction.survey_response.read` | Encuestas |
| `db_users.assignment.read` | Asignaciones |
| `db_users.permission_group.read` | Necesario para que el form "Crear estudiante" resuelva el grupo student (migr 026) |
| `db_users.school.read` | Ver su colegio |
| `db_users.users.read` / `db_users.users.write` | **Gestionar estudiantes** (crear/actualizar — migr 025). No es admin: `users.write` scopeado a estudiantes de sus colegios |

### 3.4 `marketing_permissions` (Marketing)

`analytics.simulacro_masivo.read`, `analytics.dashboard.read`, `analytics.export.write`, `db_keys.key.read`, `db_users.school.read`, `db_users.users.read`.

### 3.5 `colegio_permissions` (Cuenta del colegio)

`analytics.colegio_portal.read`, `db_users.school.read`, `db_users.users.read`. El filtro a su propio `school_id` lo garantiza el back (ver §4).

### 3.6 `coordinador_management`

Solo `db_users.coordinador.write`. Grupo dedicado y aditivo (ver §6).

### 3.7 `student_permissions`

`db_exams.exam.read`, `db_exams.exam_attempt.write` (el alumno **crea** intentos), `db_satisfaction.survey.read`, `db_satisfaction.survey_response.write`, `db_users.school.read`, `db_users.users.read`.

> `db_exams.exam_attempt.write` es el marcador de "token de estudiante": es el único rol no-admin que lo tiene. El gateway lo usa (`callerIsStudentLike`) para restringir `/api/users/search` a que el alumno solo se vea a sí mismo (cierra la enumeración de DNIs de compañeros).

---

## 4. Scope por colegio

`hasPermission` responde "¿tiene el permiso?"; el **scope por colegio** responde "¿sobre CUÁLES colegios?". Un asesor con `analytics.dashboard.read` puede ver dashboards, pero solo de **sus** colegios. Esto lo resuelven los helpers de `helpers.go`, no la lista de permisos.

### 4.1 `enforceColegioScope(r, targetSchoolID)`

Para endpoints que reciben un `school_id` en el path. Devuelve `true` si el caller puede tocar ese colegio. Política:

1. **superadmin** → cualquier colegio.
2. **admin-marker** (`db_users.permission_group.write`) → cualquier colegio (no está atado a uno).
3. **cuenta del rol Colegio** → si el `school_id` del JWT coincide con el target. **Excepción:** si el colegio está **desactivado** ("en reserva"), su cuenta-portal pierde acceso (este fast-path es exclusivo del portal).
4. **asesor / coordinador** → llama `ListSchoolsByAsesor(caller)` en el users-service y verifica que el target esté en la lista. `ListByAsesor` resuelve tanto colegios asignados como asesor (`asesor_de_colegio`) como el colegio que el usuario **coordina** (`school.user_id = caller`).

Costo: 1 RPC extra al users-service por request. Cerrado por defecto: si la consulta falla, deniega.

### 4.2 `callerColegioScope(r)` → `(unrestricted, allowed, caller)`

Para listados/búsquedas que **no** toman un `school_id` de path (`/api/users/search`, `/api/keys/search`). Devuelve:
- `unrestricted=true` para superadmin/admin (ven todo).
- `allowed`: set de `school_id` visibles (colegios del asesor/coordinador vía assignments + el `school_id` del JWT si lo tuviera).
- `caller`: el user_id (para que el usuario SIEMPRE se vea a sí mismo).

`scopeSearchResults(...)` filtra la respuesta a ese set, cerrando la fuga de PII cross-colegio.

### 4.3 Otros scopers

- `enforceAsesorScope(r, requested)`: superadmin/admin pasan cualquier `asesor_user_id`; el resto queda forzado a su propio Subject (para visitas, drill-down de keys).
- `enforceUserScope(r, target)`: superadmin/admin, o **self**. Gatea attempts, dashboards individuales, histórico y reporte vocacional de un alumno concreto.
- `callerOwnsKeyOrSuperadmin(r, keyID)`: dueño de la key (`asesor_user_id == caller`), o quien tenga el colegio de la key en scope (coordinadores/asesores que heredaron llaves), o superadmin.

---

## 5. Permisos clave por dominio (codes reales)

| Dominio (scope) | Permiso | Gatea |
| --- | --- | --- |
| **db_users** | `db_users.users.read` / `.write` | Ver / crear-editar-desactivar usuarios |
| | `db_users.school.read` / `.write` | Ver / crear-editar colegios |
| | `db_users.permission.read` / `.write` | Ver / editar el catálogo de permisos (write ≈ superadmin) |
| | `db_users.permission_group.read` / `.write` | Ver / gestionar grupos (write = **admin-marker**) |
| | `db_users.assignment.read` / `.write` | Ver / reasignar asesor-colegio-coordinador |
| | `db_users.coordinador.write` | Gestionar coordinadores de un colegio (scopeado, §6) |
| | `db_users.audit_log.read` | Audit log del users-service |
| **db_exams** | `db_exams.exam_type.read` | Catálogo de tipos de examen |
| | `db_exams.exam.read` / `.write` | Ver / crear-publicar-clonar exámenes |
| | `db_exams.question.read` / `.write` | Banco de preguntas |
| | `db_exams.exam_question.write` | Añadir/quitar/reordenar preguntas de un examen |
| | `db_exams.exam_attempt.read` / `.write` | Ver intentos / rendir (write = estudiante o admin) |
| **db_keys** | `db_keys.key.read` / `.write` | Ver / generar-editar-desactivar llaves de **colegio** |
| | `db_keys.key_usage.read` | Auditoría de uso (quién usó qué llave, cuándo) |
| | `db_keys.lan.read` / `.write` | Ver / gestionar llaves **LAN masivas** (§7) |
| **db_satisfaction** | `db_satisfaction.survey.read` / `.write` | Ver / crear-editar-publicar encuestas |
| | `db_satisfaction.survey_response.read` / `.write` | Ver respuestas / responder |
| **analytics** | `analytics.dashboard.read` | Dashboards agregados (asesor/colegio/estudiante/comparativo) |
| | `analytics.export.write` | Generar exports XLSX |
| | `analytics.colegio_portal.read` | Portal del Colegio |
| | `analytics.simulacro_masivo.read` | Apartado Simulacro Masivo |
| **hubspot** | `hubspot.contact.read` / `.write` | Lookup / upsert de contactos |
| | `hubspot.otp.write` | Disparar webhook OTP |
| | `hubspot.custom_object.write` | Upsert de Key/Asesor/Colegio en HubSpot |
| | `hubspot.jobs.read` / `.write` | Ver / reintentar jobs de sync fallidos (DLQ) |

---

## 6. Gestión de coordinadores — `db_users.coordinador.write`

**Problema que resuelve:** históricamente `AssignCoordinador`/`RevokeCoordinador` eran solo-superadmin (hardcodeado). El cliente pidió que el **asesor** pudiera gestionar los coordinadores de sus colegios, sin abrir eso a todos los asesores por default.

**Solución (migraciones 037 + 038):**
- Se creó el permiso `db_users.coordinador.write` — "Gestionar coordinadores" (asignar/quitar coordinadores de un colegio).
- La migr 037 lo horneó en `asesor_permissions` (default ON para todos). La migr 038 **revirtió** ese default: lo movió a un grupo dedicado `coordinador_management` ("Gestión de coordinadores") y lo **quitó** de `asesor_permissions`.
- Efecto neto hoy (verificado en BD): **ningún asesor tiene el permiso** hasta que el superadmin le agregue el grupo `coordinador_management`. Es aditivo — conviven `asesor_permissions` + `coordinador_management`.

**Enforcement (gateway):**
- Endpoints: `POST /api/schools/{id}/coordinadores` (asignar), `DELETE /api/schools/{id}/coordinadores/{userId}` (revocar), `GET /api/schools/{id}/coordinadores` (listar) — en `users.go`.
- `assignCoordinadorToSchool` / `revokeCoordinadorFromSchool` verifican `enforceColegioScope(r, {id})`: el asesor solo puede tocar coordinadores de **sus** colegios; admin/superadmin, de cualquiera (fix auditoría 2026-07-02).
- **Desactivar/reactivar** a un coordinador: `callerCanDeactivateCoordinador` (helpers.go) exige `db_users.coordinador.write` **y** que los colegios donde el target es coordinador intersecten los del caller. Al desactivarlo, el login lo bloquea (`users.active=false`), igual que cuando el admin desactiva a un asesor.

Así, el asesor gestiona coordinadores **scopeado** a sus colegios; el permiso lo habilita, `enforceColegioScope`/`callerColegioScope` lo acotan.

---

## 7. Caso LAN masiva — `db_keys.lan.read` / `db_keys.lan.write` (migr 039)

**Contexto:** la LAN masiva es la campaña "Prepárate" — llaves **sin colegio** (`mode='lan'`) que captan leads públicos, distintas de las llaves de simulacro de colegio.

**Por qué se separó:** hasta la migr 039, crear/editar una LAN montaba sobre el genérico `db_keys.key.write` — el **mismo** permiso de las llaves de colegio. No se podía dar "crear llaves de colegio" sin dar también "crear LAN", y un asesor podía crear LANs sin querer. Se separaron en permisos propios:

- `db_keys.lan.read` — ver/listar las llaves LAN masivas.
- `db_keys.lan.write` — crear/editar/desactivar las llaves LAN masivas.

**Qué gatea cada uno (gateway `keys.go`), discriminando por `mode`:**

| Acción | Gate |
| --- | --- |
| Crear llave con `mode == "lan"` | `db_keys.lan.write` (`keys.go` ~L163-166) |
| Crear llave de colegio (`mode != "lan"`) | `db_keys.key.write` (`keys.go` ~L168) |
| Editar/desactivar una LAN (la key existente es `mode='lan'`) | `db_keys.lan.write` (`keys.go` ~L376-388) |
| Editar/desactivar llave de colegio | `db_keys.key.write` (`keys.go` ~L391) |
| Ver una LAN (`GET` de una key `mode='lan'`) | dueño/admin **o** `db_keys.lan.read` (`keys.go` ~L348) |
| Búsqueda `/api/keys/search` | las LAN (sin colegio) se incluyen solo si el caller tiene `db_keys.lan.read` — `scopeSearchResults(..., includeLAN)` (`keys.go` ~L547) |

**Asignación:** por defecto (migr 039) ambos permisos se otorgan **solo** a `admin_permissions` (verificado en BD). El superadmin, o quien tenga `db_users.permission_group.write`, los asigna a Marketing u otros grupos desde la gestión de grupos de permisos ("Perfiles de acceso"). El scope-por-colegio no aplica a las LAN porque no tienen colegio; el `includeLAN` del filtro de búsqueda es lo que las hace visibles a quien tenga `lan.read`.

---

## 8. Nota operativa: cambiar un permiso requiere re-login

Como los permisos viajan **dentro del JWT** (§1.3) y el gateway autoriza leyendo el token —no la BD en cada request—, **cualquier cambio de permisos o de grupos NO surte efecto hasta que el usuario vuelva a iniciar sesión** (o refresque el token). Esto aplica a:

- Asignar `coordinador_management` a un asesor (§6) — no podrá gestionar coordinadores hasta re-login.
- Otorgar `db_keys.lan.read`/`.write` a Marketing (§7) — no verá/creará LANs hasta re-login.
- Cualquier alta/baja de grupo o de permiso dentro de un grupo.

El superadmin es la única excepción funcional: su bypass (`is_superadmin`) no depende de la lista `permissions` del token, sino del rol `superadmin` en los claims.

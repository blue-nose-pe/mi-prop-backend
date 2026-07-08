# Mi Propósito 2.0 — Backend

Plataforma de orientación vocacional, simulacro de admisión y hábitos de
estudio para la **UCSP** (Universidad Católica San Pablo, Arequipa).

El backend está construido como una colección de microservicios en Go que
se comunican entre sí por gRPC y exponen un único punto público vía un
gateway HTTP/REST. Toda la persistencia es **Azure SQL Database** (una base
de datos por servicio) y **Azure Cache for Redis** para cola y caché.

## 📚 Documentación

| Doc | Para qué |
|---|---|
| [`GUIA_INSTALACION_CLIENTE.md`](GUIA_INSTALACION_CLIENTE.md) | **DevOps UCSP — cómo deployar** desde cero: Azure, Bicep, DevOps, DNS, TLS, troubleshooting. |
| [`deploy/helm/miproposito/ARCHITECTURE.md`](deploy/helm/miproposito/ARCHITECTURE.md) | **Diseño del umbrella chart**: por qué cada decisión, gotchas, decisiones abiertas. Ideal para reuniones de revisión técnica. |
| [`docs/SERVICES.md`](docs/SERVICES.md) | **Referencia por microservicio**: env vars, secrets, dependencias gRPC, hooks Helm. Lo que mira el operador cuando algo no levanta. |
| [`docs/PERMISOS.md`](docs/PERMISOS.md) | **Modelo de permisos por grupos**: catálogo atómico read-only, grupos (roles), scoping por colegio, gates del gateway y el permiso LAN (`db_keys.lan.read/write`). |
| [`docs/ASISTENTE.md`](docs/ASISTENTE.md) | **Asistente IA del panel** (`/api/assistant/chat`): tool-calling scopeado por rol, catálogo de tools y de gráficos, contrato de respuesta. |
| [`deploy/asistente/CONTRATO.md`](deploy/asistente/CONTRATO.md) | Contrato técnico del endpoint del asistente + scripts de reconciliación de datos que usa (`reconcile_usos.sql`, `relink_attempts_keys.sql`). |
| [`deploy/api-docs/openapi.yaml`](deploy/api-docs/openapi.yaml) | Especificación OpenAPI 3.1 del API REST público (gateway). |
| [`CLAUDE_CONTEXT.md`](CLAUDE_CONTEXT.md) | Contexto operativo para sesiones de desarrollo asistido. |

---

## 1. Qué hace el sistema

Mi Propósito 2.0 unifica tres herramientas de evaluación bajo una misma
cuenta y un mismo conjunto de datos:

- **Vocacional** (modelo RIASEC adaptado): identifica afinidad por carreras
  agrupadas en seis dimensiones (Realista, Investigador, Artístico, Social,
  Emprendedor, Convencional).
- **Simulacro de admisión UCSP**: preguntas con opción correcta única,
  scoring por puntos (soporta puntajes decimales y reglas por prueba).
- **Hábitos / estilos de aprendizaje** (VARK): visual, auditivo,
  lecto-escritor, kinestésico.

Sobre eso se montan cuatro capacidades transversales:

- **Encuestas de satisfacción** (NPS, escalas, opciones, abiertas), con
  métricas calculadas en tiempo real. Una encuesta puede atarse a varias
  llaves de acceso (encuesta-por-key).
- **Dashboards y exports** para el equipo UCSP (asesor, colegio,
  comparativos, histórico de un estudiante; exports XLSX).
- **Asistente IA** (chatbot solo-lectura del panel): responde preguntas en
  lenguaje natural sobre los datos, **heredando el scope del rol** del
  usuario que pregunta. Ver [§7](#7-asistente-ia-del-panel).
- **Sincronización con HubSpot**: cada estudiante / asesor / colegio se
  refleja como contacto o custom object; los resultados de cada test se
  envían como propiedades del contacto.

---

## 2. Identidad y autorización

> Referencia completa y ejemplos en [`docs/PERMISOS.md`](docs/PERMISOS.md).
> Esta sección resume el modelo.

### El modelo en tres niveles

La autorización se construye en capas para que UCSP pueda darle a cada
persona exactamente el alcance que necesita, sin más:

```
permisos atómicos (catálogo fijo)  →  grupos (roles)  →  usuarios
```

1. **Permisos atómicos** — el catálogo del producto, definido por Blue
   Nose y read-only desde la API. Cada permiso describe **una acción
   sobre una tabla específica de una base de datos específica**.
2. **Grupos de permisos (roles)** — los arma el cliente combinando
   permisos atómicos. Un grupo es básicamente una etiqueta reutilizable
   ("Cargador de exámenes", "Asesor de Arequipa", "Director de colegio
   X") que agrupa el conjunto de permisos que ese rol necesita.
3. **Usuarios** — cada usuario tiene cero o más grupos asignados. Sus
   permisos efectivos son la unión de todos los grupos que tiene.

El **superadmin** está fuera de esta lógica: es un flag
(`users.is_superadmin = 1`) sobre un único usuario inicial creado por el
bootstrap del cluster. Bypassa TODOS los chequeos. Existe para arrancar
el sistema y para tener siempre una vía de recuperación; el operador
real (ej. el equipo de IT de UCSP) trabaja con un usuario distinto al
que se le asigna el grupo `admin_permissions`.

### Cómo está formado un permiso atómico

```
<base_de_datos>.<tabla>.<acción>
```

| Parte | Ejemplos | Qué representa |
|---|---|---|
| `<base_de_datos>` | `db_users`, `db_exams`, `db_keys`, `db_satisfaction`, `analytics`, `hubspot` | El microservicio / dominio de datos. |
| `<tabla>` | `users`, `school`, `exam`, `question`, `key`, `lan`, `survey`, `dashboard`, `permission_group`, `coordinador` | La entidad concreta sobre la que se actúa. |
| `<acción>` | `read`, `write` | Lectura (GET / List / Search) o escritura (POST / PATCH / DELETE). |

Algunos ejemplos del catálogo seedeado:

- `db_exams.question.write` — crear, editar y borrar preguntas del banco.
- `db_users.users.read` — listar y consultar usuarios.
- `db_keys.key.write` — generar y desactivar códigos de acceso **de colegio**.
- `db_keys.lan.read` / `db_keys.lan.write` — **ver / gestionar llaves LAN
  masivas** (Simulacro Masivo, sin colegio asociado). Un caller con
  `db_keys.lan.read` ve esas llaves en los listados aunque no sea el dueño;
  crear/editar una LAN masiva exige `db_keys.lan.write` (una llave de
  colegio sigue rigiéndose por `db_keys.key.write`).
- `db_users.coordinador.write` — permite a un **asesor** gestionar los
  coordinadores de sus colegios (permiso asignable por el superadmin;
  antes esto era solo-superadmin).
- `analytics.dashboard.read` — consultar los dashboards agregados.
- `db_users.permission_group.write` — administrar roles (crear grupos, agregar/quitar permisos).

### Por qué esa granularidad — caso de uso

Imaginá que UCSP contrata a una persona **solo para llenar el banco de
preguntas** de los exámenes vocacional / simulacro / hábitos. No querés
darle acceso a usuarios, ni a colegios, ni a resultados, ni a HubSpot.
Solo a la tabla `question` y `question_option` de `db_exams`.

Con este modelo, en tres llamadas:

```
# 1. Listar el catálogo y encontrar los IDs de los permisos relevantes
GET /api/permissions
   → encuentra: db_exams.question.read, db_exams.question.write,
                db_exams.exam.read

# 2. Crear el grupo "Cargador de exámenes" con esos permisos
POST /api/permission-groups
{
  "code": "exam_loader_permissions",
  "name": "Cargador de exámenes",
  "description": "Solo carga preguntas y opciones; no ve datos de usuarios",
  "permission_ids": [12, 13, 9]
}

# 3. Asignar el grupo al usuario contratado
POST /api/users/{user_id}/permissions/groups
{ "permission_group_id": 7 }
```

Listo. Esa persona ahora puede llamar a `POST /api/questions`,
`PATCH /api/questions/{id}`, `POST /api/exams/{id}/questions`, etc., pero
si intenta hacer un `GET /api/users/{id}` el backend devuelve
`403 PERMISSION_DENIED`. Sin tocar código ni redeployar — todo a
runtime.

### Cuatro grupos pre-seedeados de fábrica

El primer install incluye cuatro grupos canónicos como referencia. El
cliente puede modificarlos, eliminarlos o ignorarlos y armar los suyos.

| Grupo | Permisos típicos |
|---|---|
| `admin_permissions` | CRUD completo sobre tablas operacionales + administración de roles (incluye `db_users.permission_group.write`). Pensado para el operador de UCSP que reemplaza al superadmin en el día a día. |
| `asesor_permissions` | Lectura amplia + escritura sobre sus colegios y keys + ver dashboards de sus asignaciones. Puede sumar `db_users.coordinador.write` para gestionar coordinadores. |
| `coordinador_permissions` | Lectura del colegio asignado, sus estudiantes y sus resultados. |
| `student_permissions` | Leer su propio progreso, resolver tests. |

### Endpoints de administración

Roles (cualquiera con `db_users.permission_group.write` puede usarlos;
el superadmin los tiene por bypass):

```
POST   /api/permission-groups                              ← crear rol
GET    /api/permission-groups                              ← listar
GET    /api/permission-groups/{id}                         ← detalle (con permisos)
PATCH  /api/permission-groups/{id}                         ← editar nombre/descripción
DELETE /api/permission-groups/{id}                         ← eliminar (rechaza si tiene users)
POST   /api/permission-groups/{id}/permissions/{perm_id}   ← agregar permiso al rol
DELETE /api/permission-groups/{id}/permissions/{perm_id}   ← quitar permiso del rol
GET    /api/permissions                                     ← listar el catálogo (read-only)
```

Asignación de roles a un usuario:

```
POST /api/users/{id}/permissions/groups       { permission_group_id }
DELETE /api/users/{id}/permissions/groups/{permission_group_id}
GET    /api/users/{id}/permissions             ← codes efectivos del usuario
```

### Defensa en profundidad: route guards + rate limits específicos

**Filosofía de autorización**: **ningún endpoint es "superadmin-only" en
sentido fuerte**. Todos los gates son permission-based vía
`hasPermission(r, "<code>")`, que internamente bypasea para superadmin.
Así UCSP puede dar a cualquier rol custom el acceso a cualquier
recurso simplemente armando un `permission_group` con los codes
correspondientes — sin pedirnos cambios de código. Las únicas
excepciones son distinciones operacionales internas (ej. "el caller
debe ser el propio dueño de este attempt") que no son privilegios
sino reglas de negocio.

**Gateway** (Go) aplica varias capas además del JWT:

- **Permission gates por endpoint** (`hasPermission(r, "<code>")`):
  cada handler sensible de `/api/...` valida el permiso atómico que
  corresponde. Ejemplos:
  - `/api/analytics/comparativo`, `/api/analytics/colegios/historico`,
    `/api/analytics/dashboard` → `analytics.dashboard.read`
  - `/api/analytics/comparativo/export.xlsx` → `analytics.export.write`
  - `/api/exams/{id}/attempts` (list all attempts of an exam) →
    `db_exams.exam_attempt.read`
  - `/api/keys/resync-all` → `db_keys.key.write`
  - crear/gestionar una LAN masiva → `db_keys.lan.write`; verla en
    listados sin ser dueño → `db_keys.lan.read`
- **Self-scope + permission fallback**: endpoints como
  `/api/analytics/asesor/{id}/dashboard` aceptan al asesor dueño SIN
  permiso extra (self-service); para ver el dashboard de OTRO asesor
  hace falta `analytics.dashboard.read`. Lo mismo para attempts
  (`/api/attempts/{id}` requiere `db_exams.exam_attempt.read` si no
  sos el dueño).
- **Scope por colegio asignado**: endpoints como
  `/api/analytics/colegio/{id}/dashboard` o
  `POST /api/users/students` validan que el `school_id` esté en la
  lista de colegios asignados al asesor caller (vía
  `enforceColegioScope` / `ListSchoolsByAsesor`).
- **Rate limit global**: 600 req/min/IP (Redis sliding window), bucket
  `rl:ip:`.
- **Rate limit estricto en endpoints públicos sensibles**:
  `POST /api/auth/student/lookup-by-key` usa
  `RateLimitNamed(rdb, 30, "lookup")` con bucket independiente
  (`rl:lookup:`) — anti-enumeration: 30 req/min/IP, 6× lo que un
  estudiante real necesita.

**El superadmin como bypass, no como gate**: el flag
`users.is_superadmin = 1` hace que `hasPermission(r, anything)`
devuelva `true` siempre. Es un atajo operacional para el primer
install (no hay que crear el permission_group antes de poder usar la
app) y para recovery (siempre hay una vía para destrabar el sistema).
En operación normal UCSP debería trabajar con un user no-superadmin
que tenga `admin_permissions` (o un grupo similar) — el superadmin
queda como "break glass".

**Front Angular** (`ucsp-front/src/app/app.routes.ts`) aplica
`permissionGuard('<code>')` a cada ruta de `/app/*` para que un user sin
el permiso correspondiente NO vea siquiera el formulario (route guards
redirigen a `/app/dashboard`). El componente sigue verificando con
`AuthService.hasPermission()` antes de invocar la API.

### Por qué el catálogo de permisos atómicos es read-only

Cada code (`db_users.users.read`, etc.) está mapeado en el código Go a
los RPCs concretos que valida (en `permission_map.go` de cada servicio).
Inventar un code nuevo a runtime no tendría efecto: ningún RPC lo
chequearía. Por eso `GET /api/permissions` solo lista — agregar un code
nuevo es trabajo de versión y se entrega como parte de una migración
del producto.

En la práctica esto significa que **la línea entre lo que decide el
cliente y lo que decide Blue Nose está clara**: Blue Nose define qué
acciones existen sobre qué tablas; UCSP decide quién puede hacer qué.

### Login según el tipo de usuario

| Tipo de usuario | Cómo se autentica |
|---|---|
| Superadmin (un único usuario inicial) | email + password (bcrypt). |
| Cualquier otro usuario administrativo (admin / asesor / coordinador) | email + password (bcrypt). Lo crea un superadmin vía `POST /api/users` y le asigna el grupo correspondiente. |
| Estudiante (asignado al grupo `student_permissions`) | OTP de 6 dígitos enviado por email (HubSpot Workflow o Resend, ver §6). |

### Bootstrap del primer superadmin

Cuando el chart Helm se instala por primera vez, un Job `post-install`
llamado `users-service-bootstrap` corre automáticamente:

1. Genera una password aleatoria de 24 caracteres.
2. Crea el usuario `admin@ucsp.edu.pe` (configurable en
   `deploy/helm/charts/users-service/values.yaml`) con `is_superadmin = 1`
   y `must_change_password = 1`.
3. Persiste la password en un Secret de Kubernetes.

Para obtenerla después del install:

```bash
kubectl get secret <release>-users-service-superadmin -n <namespace> \
  -o jsonpath='{.data.password}' | base64 -d
```

Hasta que el superadmin haga su primer login y cambie su password, el
backend rechaza con `403 PASSWORD_CHANGE_REQUIRED` cualquier ruta que no
sea `/api/users/me`, `/api/users/me/change-password` o
`/api/auth/logout`. Esto es deliberado y aplica a cualquier usuario al
que se le haga `reset-password`.

El **flag viaja en el access token** como claim `mcp` (boolean). El
middleware `MustChangePasswordGuard` del gateway lo lee y bloquea
endpoints no permitidos. El **front Angular** (`ucsp-front`) lo expone
vía `AuthService.mustChangePassword()` y un guard global
(`mustChangePasswordGuard`) aplicado al árbol `/app/*` redirige a
`/app/cambiar-password` en cuanto el flag está activo. La pantalla forza
ingresar la contraseña actual + nueva (mínimo 12 caracteres) y
post-update fuerza un logout para que el siguiente login emita un JWT
limpio (sin `mcp`).

### Flujo de login

**Admin / asesor / coordinador**:

```
POST /api/auth/login        { email, password }   →  { user, permissions, access_token, refresh_token }
POST /api/auth/refresh      { refresh_token }     →  tokens nuevos (rotación: el viejo queda revocado)
POST /api/auth/logout       { refresh_token }     →  204 (requiere Bearer del access)
```

Access token: 15 minutos (HS256, claim `iss=miproposito.users`).
Refresh token: 7 días, rotado en cada uso. Si un atacante reusa un refresh
revocado, el servidor lo detecta y rechaza con `INVALID_REFRESH_TOKEN`.

**Estudiante**:

```
POST /api/auth/student/request-otp  { email }            →  200 (silencioso anti-enumeration)
POST /api/auth/student/verify-otp   { email, otp }       →  { user, permissions, tokens }
```

El OTP se entrega por email (HubSpot Workflow o Resend, ver §6). El backend
NO envía emails con SMTP propio. El OTP vive 10 minutos, permite 3 intentos
incorrectos antes de invalidarse, y solo hay un OTP activo por usuario
simultáneamente.

---

## 3. Microservicios

| Servicio | Puerto | DB | Responsabilidad |
|---|---|---|---|
| `gateway` | HTTP 8080 | — | Único endpoint público. Traduce REST→gRPC. JWT, CORS, rate-limit (Redis sliding window). Hospeda el proxy del **asistente IA** (`/api/assistant/chat`). |
| `users_service` | 50051 | `db_users` | Identidad, password (bcrypt), JWT, refresh tokens, permisos, OTP, schools, asignaciones (histórico SCD-2), **coordinadores muchos-a-muchos** (assignment `coordinador_de_colegio`), audit log. |
| `exams_service` | 50052 | `db_exams` | Tipos de examen (`vocacional`, `simulacro`, `habitos`), versiones por linaje (`parent_exam_id`), preguntas, opciones, attempts, scoring (decimal). Aplica el límite de **intentos por estudiante** de la llave. |
| `keys_service` | 50053 | `db_keys` | Códigos de acceso a tests, modos `school` / `lan`, ventanas temporales, **aforo por alumnos distintos** (contador atómico), `max_attempts_per_user`. |
| `hubspot_service` | 50054 + HTTP webhook 8080 | Redis (asynq) | Sync con HubSpot. Server (sync) + worker (async con backoff exponencial 1-2-4-8-16s). Rate limiter coordinado vía Redis. |
| `satisfaction_service` | 50055 | `db_satisfaction` | Encuestas, 5 tipos de pregunta (`scale`, `nps`, `single`, `multi`, `open`), métricas en runtime (NPS, average, distribución). Encuesta-por-key (`survey_key`). |
| `analytics_service` | 50056 | — (Redis para caché) | Dashboards agregados (asesor / colegio / estudiante / comparativo / histórico), exports XLSX. Lee de los demás servicios por gRPC. Es la fuente de datos del asistente IA. |

Todos siguen arquitectura **hexagonal + CQRS**: `internal/core/{domain, ports, command, query}` puro; `internal/adapters/{inbound, outbound}` con la I/O. El stack es Go 1.24, gRPC + protobuf (regenerado con `buf`), driver oficial de Microsoft para SQL Server, `golang-jwt/jwt/v5`, `redis/go-redis/v9`, `hibiken/asynq`.

> El **front Angular** (`ucsp-front/`) vive en el mismo repo pero se
> despliega aparte (ver notas de deploy del front); no es un microservicio
> backend.

---

## 4. Semánticas de negocio vigentes

Estas reglas están implementadas HOY y conviene tenerlas presentes al
leer dashboards, reportes o el asistente:

- **Aforo por alumnos distintos.** El `max_uses` de una llave se consume
  por **alumnos únicos**, no por intentos. El gate es atómico en
  `keys_service` (`UPDATE ... SET current_uses = current_uses + 1 WHERE
  ... AND (max_uses = 0 OR current_uses < max_uses)`), y un mismo alumno
  que reintenta NO vuelve a restar aforo (se cuenta `DISTINCT user_id`
  sobre `key_usage`). `max_uses = 0` significa **sin aforo** (ilimitado).
- **Intentos por estudiante configurables en la llave** (`max_attempts_per_user`).
  Los reintentos de un mismo alumno están gobernados por este campo a
  nivel `exams_service`; no consumen aforo. `0` = sin tope explícito.
- **Intento PRINCIPAL = el ÚLTIMO presentado.** Cuando un alumno rinde
  varias veces, el bot, el panel y los reportes toman como resultado
  "oficial" el más reciente, no el primero ni el mejor.
- **Colegio inactivo se resta de TODO.** `school.active = 0` saca al
  colegio (y sus llaves/alumnos/resultados) del scope del asistente, del
  reporte de asesores y de los dashboards/pendientes de analytics; además
  bloquea el registro de nuevos alumnos y el acceso al portal.
- **Coordinadores muchos-a-muchos.** Un coordinador puede estar asignado a
  varios colegios y un colegio puede tener varios coordinadores (assignment
  `kind=coordinador_de_colegio`, con `AddSource` / `RevokeSource` por par,
  reutilizables). Ya no rige la restricción única antigua `uk_school_user`.

---

## 5. Variables de entorno y secrets

### Comunes a todos los servicios

| Variable | Origen | Descripción |
|---|---|---|
| `GRPC_PORT` (o `HTTP_PORT` en gateway) | ConfigMap | Formato `:50051`. |
| `JWT_SECRET` | Key Vault | El mismo en TODOS los servicios. Gateway emite, los demás validan. 64 chars recomendado. |
| `JWT_ISSUER` | ConfigMap | `miproposito.users` por defecto. |

### Servicios con base de datos

| Variable | Notas |
|---|---|
| `SQL_SERVER`, `SQL_PORT`, `SQL_DATABASE`, `SQL_USER` | ConfigMap. Una base por servicio. |
| `SQL_PASSWORD` | Key Vault, una por servicio: `users-service-sql-password`, `exams-service-sql-password`, `keys-service-sql-password`, `satisfaction-service-sql-password`. |
| `SQL_ENCRYPT=true`, `SQL_TRUST_SERVER_CERT=false` | Por defecto en Azure SQL gestionado. |

### Servicios con Redis (gateway, users, hubspot, analytics)

| Variable | Notas |
|---|---|
| `REDIS_ADDR` | `host:port`. |
| `REDIS_PASSWORD` | Key Vault: `<svc>-redis-password`. |
| `REDIS_DB` | DBs separadas: gateway 0, users 0, hubspot 0 (asynq) + `REDIS_DB_RATELIMIT` (rate limiter), analytics 1 (caché). |
| `REDIS_TLS` | `true` en Azure Cache; `false` en cluster. |

### Por servicio (variables propias)

**gateway**
- `CORS_ALLOWED_ORIGINS` — CSV con dominios permitidos.
- `RATELIMIT_ENABLED=true`, `RATELIMIT_PER_IP_PER_MIN=600`.
- `USERS_SERVICE_ADDR`, `EXAMS_SERVICE_ADDR`, `KEYS_SERVICE_ADDR`, `HUBSPOT_SERVICE_ADDR`, `SATISFACTION_SERVICE_ADDR`, `ANALYTICS_SERVICE_ADDR` — direcciones gRPC de los upstreams (`<release>-<svc>-service:<puerto>`).
- `OPENAI_API_KEY` (Secret `assistant-openai`) — usada por el proxy del asistente IA para el tool-calling. Ver [§7](#7-asistente-ia-del-panel).
- `GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn` — necesario porque el gateway importa protos de los seis servicios y todos registran `common/error.proto`.

**users_service**
- `BCRYPT_COST=10`.
- `JWT_ACCESS_TTL=15m`, `JWT_REFRESH_TTL=168h`.
- `HUBSPOT_SERVICE_ADDR` — para enviar OTP por email.
- `OTP_SENDER` — `hubspot` (default) o `resend` (ver §6).

**exams_service**
- `KEYS_SERVICE_ADDR` — cliente gRPC a keys-service para validar/incrementar uso de keys.
- `GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn`.

**hubspot_service**
- `HUBSPOT_API_TOKENS` — Key Vault, CSV de Private App Tokens. Más tokens = más rate-limit total (cada uno 10 rps).
- `HUBSPOT_OTP_WEBHOOK_TOKEN` — Key Vault, token del Workflow trigger.
- `HUBSPOT_OTP_WEBHOOK_TRIGGER_ID` — id numérico del trigger.
- `HUBSPOT_CO_KEY_ID`, `HUBSPOT_CO_ASESOR_ID`, `HUBSPOT_CO_COLEGIO_ID` — IDs de los Custom Objects en el portal.
- `HUBSPOT_RPS=10`, `HUBSPOT_ENGINE_WORKERS`, `HUBSPOT_ENGINE_QUEUE_SIZE`.
- `HUBSPOT_ASESOR_TEAM_ID`, `HUBSPOT_ASESOR_ROLE_ID` — para invitar asesores como usuarios HubSpot.
- `WEBHOOK_HTTP_PORT=:8080` — puerto del HTTP server que recibe webhooks de HubSpot.

**analytics_service**
- `USERS_SERVICE_ADDR`, `EXAMS_SERVICE_ADDR`, `KEYS_SERVICE_ADDR`, `SATISFACTION_SERVICE_ADDR`.
- `CACHE_ENABLED=true`.

### Resumen de secrets que el Key Vault / cluster debe tener

```
users-service-sql-password
exams-service-sql-password
keys-service-sql-password
satisfaction-service-sql-password

users-service-redis-password
gateway-redis-password
hubspot-service-redis-password
analytics-service-redis-password

users-service-jwt-secret           # único, compartido por todos los servicios

hubspot-service-api-tokens          # CSV de PATs
hubspot-service-otp-webhook-token   # token del Workflow trigger

assistant-openai                    # API key OpenAI del asistente IA (gateway)
miproposito-resend-credentials      # API key Resend (si OTP_SENDER=resend)
```

Las dependencias entre servicios son las siguientes (todas por gRPC):

- `gateway` → todos los demás (+ salida HTTPS a OpenAI para el asistente).
- `exams_service` → `keys_service` (validar / incrementar key).
- `users_service` → `hubspot_service` (enviar OTP).
- `analytics_service` → `users_service`, `exams_service`, `keys_service`, `satisfaction_service` (consultas para dashboards).
- `hubspot_service` → externo (api.hubapi.com).

---

## 6. Envío del OTP del estudiante: HubSpot vs Resend

El sistema soporta dos backends de delivery del OTP, intercambiables vía
env var `OTP_SENDER` en `users_service`:

| Valor | Cómo manda el correo | Cuándo usar |
|---|---|---|
| `hubspot` (default) | Llama `hubspot_service.SendOTP` que upsertea el contacto en HubSpot y dispara el Automation Workflow. Depende de que el portal tenga "auto-set new contacts as marketing" activo o tokens con scope `marketing-contacts` / `transactional-email`. | Si el cliente prefiere mantener todo el routing dentro de su CRM HubSpot. |
| `resend` | Llama directo a `api.resend.com/emails` con la API key del secret `miproposito-resend-credentials`. Independiente de la config de HubSpot. El sync de contacto al CRM se hace best-effort en goroutine (`hubspot_service.SyncStudentContact`) sin bloquear el login. | Si el portal HubSpot no entrega correos confiablemente (caso UCSP 2026-05). |

En prod actualmente `OTP_SENDER=resend`. La cuenta Resend es de **Blue Nose**
(transición pendiente: UCSP debe crear su propia cuenta y darnos la key
para el handover final). El From sandbox es `onboarding@resend.dev`; para
prod requiere verificar `ucsp.edu.pe` en Resend (SPF TXT + DKIM CNAME) y
cambiar `RESEND_FROM` en el chart.

---

## 7. Asistente IA del panel

> Detalle completo, catálogo de tools y contrato de respuesta en
> [`docs/ASISTENTE.md`](docs/ASISTENTE.md) y
> [`deploy/asistente/CONTRATO.md`](deploy/asistente/CONTRATO.md).

El panel incluye un **chatbot solo-lectura** que responde preguntas en
lenguaje natural sobre los datos de la plataforma (rendidos, rankings,
comparativos entre llaves, satisfacción, coordinadores, campañas masivas,
etc.), devolviendo texto y **gráficos embebidos** (ApexCharts).

- **Endpoint**: `POST /api/assistant/chat`, servido por el gateway
  (`gateway/internal/proxy/assistant.go`). Se consume desde
  `/app/asistente` en el front.
- **Modelo**: tool-calling de OpenAI (API key en el Secret
  `assistant-openai`). El modelo **nunca ejecuta SQL crudo**: solo puede
  invocar un conjunto de *tools* acotadas.
- **Scope heredado por rol** — clave de seguridad: cada tool corre con el
  mismo scoping que el usuario que pregunta. Un asesor solo obtiene datos
  de sus colegios; pedir "toda la plataforma" o un colegio ajeno se
  responde con lo permitido, no con una fuga. El asistente no puede ver ni
  más ni menos que lo que el rol vería en el panel.
- **Determinismo en preguntas compuestas**: las tools que combinan varias
  fuentes (comparativos multi-año, rankings por tipo, gráficos
  personalizados con whitelist de tipos/límites) devuelven el resultado ya
  compuesto, para que el modelo no tenga que "inventar" la agregación.

---

## 8. Documentación de la API REST

La especificación OpenAPI 3.1 vive en
[`deploy/api-docs/openapi.yaml`](deploy/api-docs/openapi.yaml). El render
HTML usa **Stoplight Elements** (CDN, sin build).

### Verla localmente

```powershell
cd deploy/api-docs
python -m http.server 8000
# Abrir http://localhost:8000
```

### Servirla desde el cluster

Hay un chart Helm dedicado en `deploy/helm/charts/api-docs/`. Es un
Deployment de `nginx-alpine` que monta el `index.html` y el `openapi.yaml`
desde un ConfigMap. Antes de instalar (o cuando se actualice el spec):

```powershell
cp deploy/api-docs/index.html  deploy/helm/charts/api-docs/files/
cp deploy/api-docs/openapi.yaml deploy/helm/charts/api-docs/files/

helm install api-docs deploy/helm/charts/api-docs -n miproposito
```

Por defecto se publica en `api-docs.miproposito.ucsp.edu.pe` con TLS
automático vía cert-manager (apunta el DNS a la IP del Ingress).

---

## 9. Instalación del backend en Azure (resumen para DevOps)

> Esta sección es una referencia para que un equipo DevOps con experiencia
> en Azure pueda armar el entorno completo desde el código entregado. **No
> incluye la automatización de infraestructura** (Bicep templates, scripts
> PowerShell de instalación) — eso es un módulo separado. Si el equipo
> necesita ese atajo, contactar a Blue Nose.

### Prerrequisitos

1. Suscripción Azure con permisos de **Contributor**.
2. **Azure Kubernetes Service** con Azure CNI, addon
   `azure-keyvault-secrets-provider`, OIDC issuer y Workload Identity.
3. **Azure Container Registry** vinculado al AKS.
4. **Azure Key Vault** con todos los secrets de la sección anterior cargados.
5. **Azure SQL Database**: un servidor con cuatro bases vacías (`db_users`,
   `db_exams`, `db_keys`, `db_satisfaction`). El usuario admin necesita
   permisos de DDL — los servicios crean sus tablas en el primer arranque
   vía Jobs `*-migrate` con migraciones T-SQL embebidas (idempotentes,
   checksum SHA-256 por archivo).
6. **Azure Cache for Redis** o un Redis interno al cluster. Las DBs lógicas
   se separan por servicio.
7. **NGINX Ingress Controller** + **cert-manager** + **ClusterIssuer
   Let's Encrypt** si querés TLS automático.
8. (Opcional pero recomendado) Cuenta **HubSpot** con un Private App Token
   y los tres Custom Objects creados (Keys, Colegios, Asesores).
9. **API key de OpenAI** en el Secret `assistant-openai` si se quiere el
   asistente IA operativo.

### Pasos

**1. Construir y subir las imágenes a ACR**

Cada servicio tiene su `Dockerfile` multi-stage. Para `gateway`,
`users_service`, `exams_service` y `analytics_service`, el contexto del
build es la **raíz del repo** (importan protos de hermanos vía `go.work`):

```bash
TAG=v1.0.0
ACR=miacr.azurecr.io

# Servicios con contexto raíz:
for svc in gateway users_service exams_service analytics_service; do
  docker build --target server -t $ACR/${svc//_/-}:$TAG -f $svc/Dockerfile .
done

# Migrate y bootstrap (solo users + exams + keys + satisfaction):
docker build --target migrate -t $ACR/users-service-migrate:$TAG -f users_service/Dockerfile .
docker build --target bootstrap -t $ACR/users-service-bootstrap:$TAG -f users_service/Dockerfile .
docker build --target migrate -t $ACR/exams-service-migrate:$TAG -f exams_service/Dockerfile .
# ... idem para keys-service y satisfaction-service (estos sí usan su propio contexto):
docker build --target server  -t $ACR/keys-service:$TAG keys_service
docker build --target migrate -t $ACR/keys-service-migrate:$TAG keys_service
docker build --target server  -t $ACR/satisfaction-service:$TAG satisfaction_service
docker build --target migrate -t $ACR/satisfaction-service-migrate:$TAG satisfaction_service

# hubspot-service tiene server + worker:
docker build --target server -t $ACR/hubspot-service:$TAG hubspot_service
docker build --target worker -t $ACR/hubspot-service-worker:$TAG hubspot_service
```

**2. Cargar secrets en Key Vault** (lista en sección 5).

**3. Configurar Workload Identity Federation** entre el ServiceAccount del
namespace de la app y la Managed Identity del Key Vault Secrets Provider
(esto permite a los pods leer del Key Vault sin secretos compartidos).

**4. Editar `deploy/helm/miproposito/values.production.yaml`** con FQDNs
de tu Azure SQL, Redis, ACR y los hostnames de Ingress.

**5. Instalar el chart umbrella**:

```bash
helm dependency update deploy/helm/miproposito

helm install miproposito deploy/helm/miproposito \
  -n miproposito --create-namespace \
  -f deploy/helm/miproposito/values.production.yaml \
  --set-string global.imageTag=$TAG \
  --set-string global.imageRegistry=$ACR
```

El chart corre los Jobs `*-migrate` como hooks `pre-install/pre-upgrade`
y el Job `users-service-bootstrap` como `post-install`.

**6. Obtener la password del superadmin** (sección 2).

**7. Smoke check**:

```bash
curl https://api.miproposito.ucsp.edu.pe/health

curl -X POST https://api.miproposito.ucsp.edu.pe/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@ucsp.edu.pe","password":"<la-password-del-secret>"}'
```

**8. (Opcional) Deploy del api-docs** — sección 8.

### Operación

- **Logs** de un servicio: `kubectl logs -f deploy/miproposito-<svc> -n miproposito`.
- **Reiniciar tras cambiar un secret en Key Vault**:
  `kubectl rollout restart deploy/miproposito-<svc> -n miproposito`.
  El CSI driver rota cada cinco minutos por defecto, pero el rollout
  forzado lo aplica al instante.
- **Re-aplicar migraciones SQL**: borrar el Job y `helm upgrade --install`
  lo recrea (el migrator es idempotente).
- **Reset de password** de un usuario: el superadmin invoca
  `POST /api/users/{id}/reset-password` y el response trae la password
  temporal una sola vez. El usuario es marcado con `must_change_password=1`.

### Gotchas de deploy aprendidos

- **Dockerfiles multi-target**: `keys_service`, `exams_service`,
  `users_service` y `hubspot_service` tienen targets `server` + `migrate`
  (y users además `bootstrap`, hubspot además `worker`). **Siempre pasar
  `--target server` al `az acr build`** o el último FROM gana — y el
  último suele ser `migrate`, lo que deja el pod arrancando, corriendo
  migraciones y terminando como `Completed` sin servir gRPC.
- **Verificar que el deployment NO esté pinned a SHA digest** antes de
  hacer rollout post-build. Si está pinned, `kubectl rollout restart`
  re-pull el MISMO digest viejo aunque el `:latest` tenga código nuevo.
  Comando para auditar:
  ```bash
  for svc in keys-service exams-service users-service hubspot-service \
             analytics-service gateway; do
    img=$(kubectl get deployment miproposito-$svc -n miproposito \
          -o jsonpath='{.spec.template.spec.containers[0].image}')
    case "$img" in
      *@sha256:*) echo "$svc: PINNED -> $img" ;;
      *) echo "$svc: tag    -> $img" ;;
    esac
  done
  ```
  Para des-pinnear:
  ```bash
  kubectl set image deployment/miproposito-<svc> -n miproposito \
    <svc>=mipropositoacr.azurecr.io/<svc>:latest
  ```
- **Context de build**:
  - `gateway`, `users_service`, `exams_service`, `analytics_service` →
    context = **raíz del repo** (importan protos hermanos vía `go.work`).
  - `keys_service`, `satisfaction_service`, `hubspot_service` →
    context = su propio directorio.
- **Cache mutable tag (`:latest`, `:bulk-sync`)** con `imagePullPolicy:
  Always`: tras `az acr build` hay que hacer `kubectl rollout restart`
  para que K8s detecte el cambio (sino el pod sigue corriendo el
  ImageID del último pull).
- **Deploy de scoring/simulacro EN SYNC**: un cambio de wire en el proto
  de scoring exige redeployar los tres servicios afectados juntos
  (gateway + exams + keys) para no romper la compatibilidad binaria.

---

## Contacto

Pablo Pérez · pablo.perez@bluenose.pe · Blue Nose

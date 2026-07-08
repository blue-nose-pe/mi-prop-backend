# Dependencias por microservicio — referencia operativa

> Para cada microservicio: qué hace, qué BD usa, a quién llama por gRPC, qué env
> vars / secrets consume y qué RPCs/endpoints expone. La forma general del
> despliegue (umbrella, CSI driver, ingress) vive en
> [`../deploy/helm/miproposito/ARCHITECTURE.md`](../deploy/helm/miproposito/ARCHITECTURE.md).
> La operación cotidiana (cómo deployar, rotar, debuggear) vive en
> [`../GUIA_INSTALACION_CLIENTE.md`](../GUIA_INSTALACION_CLIENTE.md).
>
> **Fuentes de verdad de este doc:** `*/config/config.go` (env vars reales que lee
> cada binario), `*/cmd/server/main.go` (RPCs registrados + clientes gRPC), y los
> charts `deploy/helm/charts/*`. Verificado contra código a 2026-07-07.

---

## Mapa de un golpe de vista

```
┌─────────────────────────────────────────────────────────────────────┐
│  Azure Key Vault — secretos al nivel "Mi Propósito"                 │
│  ──────────────────────────────────────────────────                 │
│  users-service-sql-password          users-service-redis-password   │
│  exams-service-sql-password          gateway-redis-password         │
│  keys-service-sql-password           hubspot-service-redis-password │
│  satisfaction-service-sql-password   analytics-service-redis-password│
│  users-service-jwt-secret  (compartido por los 7 servicios)         │
│  hubspot-service-api-tokens          hubspot-service-otp-webhook-token│
│                                                                      │
│  Secrets manuales (fuera del umbrella):                             │
│  miproposito-smtp-credentials  (OTP por SMTP/Gmail — path actual)   │
│  assistant-openai / OPENAI_API_KEY  (chatbot del gateway)           │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼   (Workload Identity + CSI driver)
┌─────────────────────────────────────────────────────────────────────┐
│                            AKS                                       │
│                                                                      │
│   users-service ─────┐          ┌──────────────→ HubSpot API        │
│   exams-service ─────┤          │  ┌───────────→ SMTP (Gmail)  [OTP] │
│   keys-service ──────┼─→ Azure SQL (4 BDs)      │                     │
│   satisfaction-service                          │                     │
│                                                                      │
│   users-service ──────→ hubspot-service (gRPC, sync contacto + OTP)  │
│   keys-service ───────→ hubspot-service (gRPC, sync de la key)  ✱nuevo│
│   exams-service ──────→ keys-service    (gRPC, gate de aforo atómico)│
│   analytics-service ──→ users + exams + keys (gRPC) + Redis (cache)  │
│   hubspot-service ────→ Redis (asynq queue) + HubSpot API            │
│   gateway ────────────→ TODOS (gRPC) + Redis (rate-limit) + LLM API  │
│                          └─ asistente IA (chatbot panel) vive aquí   │
└─────────────────────────────────────────────────────────────────────┘
```

**Cambios transversales recientes (dónde vive cada uno):**
- **Asistente IA / chatbot** del panel: vive **en el gateway** (`internal/proxy/assistant.go`,
  endpoint `POST /api/assistant/chat`). Llama a un LLM estilo OpenAI (OpenAI o Ollama
  local) y a las tools scopeadas por rol; nunca ejecuta SQL crudo.
- **Filtro `active = 1` de intentos por colegio**: vive en **exams-service**
  (`ListByColegio`, `attempt_repo.go`). Excluye del reporte los intentos de alumnos
  desactivados (cuentas QA/prueba).
- **OTP del alumno**: hoy sale por **SMTP (Gmail)** desde users-service
  (`OTP_SENDER=smtp`); el path por HubSpot Workflow y el de Resend quedaron como
  legacy/código muerto.
- **Permisos LAN / Simulacro Masivo ("Prepárate")**: se gatean **en el gateway**
  (`landing.go`, permiso `analytics.simulacro_masivo.read`).

---

## Convenciones (válidas para los 7 servicios)

| Concepto | Cómo se materializa |
|---|---|
| **Puerto interno** | `GRPC_PORT=:50051..50056` (gateway usa `HTTP_PORT=:8080`; hubspot expone además `WEBHOOK_HTTP_PORT=:8080`) |
| **JWT secret** | Compartido — todos leen `users-service-jwt-secret` del KV con alias `jwt-secret` |
| **JWT issuer** | `miproposito.users` (default en código y en todos los Deployments) |
| **Config** | Cada servicio la carga en `config/config.go` vía `os.LookupEnv` con defaults; las inyecta el Deployment/Secret de K8s |
| **Imagen Docker** | `<registry>/<service>:<tag>`, registry y tag vienen de `global.*` |
| **Secret en cluster** | `miproposito-<service>-kv` (Opaque, sincronizado desde KV por SecretProviderClass) |
| **Health check** | gRPC `grpc.health.v1.Health/Check` — todos registran `RegisterHealthServer`; probes apuntan ahí |
| **Warning protos** | `GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn` (silencia el warning de protos compartidos) |

Servicios y puertos:

| Servicio | Puerto gRPC | BD propia | Redis | Llama por gRPC a |
|---|---|---|---|---|
| `users-service` | `:50051` | `db_users` | sí (cache) | hubspot-service |
| `exams-service` | `:50052` | `db_exams` | no | keys-service |
| `keys-service` | `:50053` | `db_keys` | no | hubspot-service (opcional) |
| `hubspot-service` | `:50054` (+`:8080` webhook) | — (usa Redis/asynq) | sí (asynq + rate-limit) | — |
| `satisfaction-service` | `:50055` | `db_satisfaction` | no | — |
| `analytics-service` | `:50056` | — (todo on-demand) | sí (cache) | users, exams, keys |
| `gateway` | `:8080` (HTTP) | — | sí (rate-limit) | los 6 + LLM externo |

---

## 1. `users-service`

**Qué hace:** identidad y autorización — login con password, refresh tokens, OTP
para estudiantes, permission groups, colegios, asesores/coordinadores, leads,
visitas y audit. Es la fuente de verdad de quién es quién.

**BD:** `db_users` (Azure SQL). Job de migración `users-service-migrate`.

**Necesita corriendo:** Azure SQL (`db_users`), Redis (cache de user lookup),
`hubspot-service` (sync de contacto student al CRM, best-effort). Para OTP por
correo: un servidor SMTP (Gmail).

**Dependencias gRPC (salientes):** `hubspot-service` (`NewHubspotServiceClient` →
SendOTP legacy / SyncStudentContact / UpsertContact).

**Imagen:** `users-service:<tag>` (binario `server`) — Dockerfile multi-target;
produce también `users-service-migrate` (Job) y `users-service-bootstrap` (Job).

### RPCs que expone (registrados en `cmd/server/main.go`)
`AuthService`, `UserService`, `SchoolService`, `PermissionGroupService`,
`LeadService`, `VisitaService`, `Health`.

### Variables de entorno (`config/config.go` + `deployment.yaml`)

| Variable | Origen | Valor / Notas |
|---|---|---|
| `GRPC_PORT` | env (def `:50051`) | `:50051` |
| `SQL_SERVER` / `SQL_PORT` | `global.*` | Azure SQL FQDN (prod) o `mssql-server.miproposito.svc…` (staging); `1433` |
| `SQL_DATABASE` | values | `db_users` |
| `SQL_USER` | values | `miprop_admin` (default de código `sa`) |
| `SQL_PASSWORD` | KV secret | `users-service-sql-password` → alias `sql-password` |
| `SQL_ENCRYPT` / `SQL_TRUST_SERVER_CERT` | `global.*` | `true` / `false` en prod |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_TLS` / `REDIS_DB` | `global.*` + KV | pass = `users-service-redis-password` → `redis-password`; DB `0` |
| `CACHE_TTL` | env (def `15m`) | TTL del cache de usuarios |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | env (def `miproposito.users`) | `miproposito.users` |
| `JWT_ACCESS_TTL` / `JWT_REFRESH_TTL` | env (def `15m` / `168h`) | access token / refresh token |
| `BCRYPT_COST` | env (def `10`) | `12` en prod recomendado |
| `HUBSPOT_SERVICE_ADDR` | derivado | `<release>-hubspot-service:50054`; vacío ⇒ OTP sender NoOp (dev) |
| `OTP_SENDER` | values (`smtp`) | `smtp` (**default de despliegue, path actual**) · `hubspot` (Workflow legacy) · default de código `hubspot`. **`resend` retirado — código muerto.** |
| `SMTP_HOST` / `SMTP_PORT` / `SMTP_FROM` | values | `smtp.gmail.com` / `587` / `"Mi Proposito UCSP <…@gmail.com>"` |
| `SMTP_USERNAME` / `SMTP_PASSWORD` | Secret manual | de `miproposito-smtp-credentials` (App Password de Gmail), **no en Helm** (ver §9) |
| `FRONT_BASE_URL` | env (opcional) | base pública del front para el magic-link del correo MASIVO; `""` ⇒ el correo solo lleva el código |
| `GOLANG_PROTOBUF_REGISTRATION_CONFLICT` | hardcoded | `warn` |

> **Legacy inerte:** el `deployment.yaml` todavía cablea `RESEND_FROM` /
> `RESEND_REPLY_TO` / `RESEND_API_KEY` (desde `…-kv`), pero `config.go` ya **no**
> los lee. Son inofensivos; se limpiarán en un pase futuro.

### Secrets que necesita en Key Vault
- `users-service-sql-password`
- `users-service-redis-password`
- `users-service-jwt-secret` (lo leen también los otros 6 servicios)
- (SMTP creds NO están en KV — Secret manual `miproposito-smtp-credentials`, §9)

### Hooks Helm
- **pre-install/pre-upgrade:** `Job users-service-migrate` — migraciones T-SQL sobre `db_users`.
- **post-install:** `Job users-service-bootstrap` — crea el primer superadmin. **Ya
  NO genera password random**: lee `SUPERADMIN_EMAIL` y `SUPERADMIN_PASSWORD` del
  Job (`cmd/bootstrap/main.go`, ambos obligatorios) y crea/actualiza esa cuenta.

---

## 2. `exams-service`

**Qué hace:** catálogo de exámenes, preguntas, opciones; attempts (intentos del
estudiante), respuestas y scoring. Antes de crear un attempt hace el **gate atómico
de aforo** contra keys-service.

**BD:** `db_exams` (Azure SQL). **No usa Redis.**

**Dependencias gRPC (salientes):** `keys-service` (`NewKeyServiceClient` →
ValidateKey / consumo de aforo por alumno distinto).

**Imagen:** `exams-service:<tag>` — Dockerfile multi-target (server + migrate).

### RPCs que expone
`ExamService`, `QuestionService`, `ExamQuestionService`, `AttemptService`, `Health`.

> **Filtro `active = 1`:** `AttemptService.ListByColegio` (repo
> `attempt_repo.go`) excluye intentos de alumnos desactivados (cuentas QA/prueba),
> para que el reporte por colegio no cuente basura.

### Variables de entorno

| Variable | Origen | Valor |
|---|---|---|
| `GRPC_PORT` | env (def `:50052`) | `:50052` |
| `SQL_SERVER` / `SQL_PORT` / `SQL_ENCRYPT` / `SQL_TRUST_SERVER_CERT` | `global.*` | igual que users |
| `SQL_DATABASE` | values | `db_exams` |
| `SQL_USER` | values | `miprop_admin` |
| `SQL_PASSWORD` | KV secret | `exams-service-sql-password` → `sql-password` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | env (def `miproposito.users`) | `miproposito.users` |
| `KEYS_SERVICE_ADDR` | derivado | `<release>-keys-service:50053` |
| `GOLANG_PROTOBUF_REGISTRATION_CONFLICT` | hardcoded | `warn` |

### Secrets en Key Vault
- `exams-service-sql-password`
- `users-service-jwt-secret`

### Hooks Helm
- **pre-install/pre-upgrade:** `Job exams-service-migrate`.

---

## 3. `keys-service`

**Qué hace:** códigos de acceso (keys) que los asesores entregan a estudiantes —
validación, vigencia, aforo por alumnos DISTINTOS (los reintentos del mismo alumno
no consumen cupo). Permite atar una key a un `exam_id` puntual y a una encuesta.

**BD:** `db_keys` (Azure SQL). **No usa Redis.**

**Dependencias gRPC (salientes):** `hubspot-service` (**nuevo** —
`NewHubspotServiceClient`): tras crear/editar una key la replica al CRM (`SyncKey`).
Si `HUBSPOT_SERVICE_ADDR` está vacío cae a `NoopSyncer` (no replica). Fuera de eso,
keys-service no depende de nadie.

**Imagen:** `keys-service:<tag>` — Dockerfile multi-target (server + migrate).

### RPCs que expone
`KeyService`, `Health`.

### Variables de entorno

| Variable | Origen | Valor |
|---|---|---|
| `GRPC_PORT` | env (def `:50053`) | `:50053` |
| `SQL_*` | `global.*` + values | `db_keys` |
| `SQL_PASSWORD` | KV secret | `keys-service-sql-password` → `sql-password` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | env (def `miproposito.users`) | `miproposito.users` |
| `HUBSPOT_SERVICE_ADDR` | derivado (opcional) | `<release>-hubspot-service:50054`; vacío ⇒ NoopSyncer |

### Secrets en Key Vault
- `keys-service-sql-password`
- `users-service-jwt-secret`

### Hooks Helm
- **pre-install/pre-upgrade:** `Job keys-service-migrate`.

---

## 4. `hubspot-service`

**Qué hace:** sincronización con HubSpot CRM — upsert de contactos, custom objects
(Key, Asesor, Colegio), leads de la landing masiva, envío de OTP vía Workflow (path
legacy) y webhooks entrantes. Tiene **2 binarios** (`server` y `worker`) → **2
Deployments**.

**BD:** ninguna SQL propia; su estado de trabajo es la cola **asynq en Redis**.

**Dependencias gRPC (salientes):** ninguna. (Ya **no** dial-ea users-service; es
users/keys quienes lo invocan a él.) Depende de la **API externa de HubSpot** y de
Redis.

**Imagen:** `hubspot-service:<tag>` con `--target server` y `--target worker`.

### RPCs que expone
`HubspotService`, `Health`. El **server** además escucha HTTP en `WEBHOOK_HTTP_PORT`
para los webhooks de HubSpot.

### RPCs sin JWT (skip-list intra-cluster, `cmd/server/main.go`)
Son ClusterIP; "sin JWT" = callers internos anónimos (flujo del alumno / landing):
- `SendOTP`
- `UpsertContact`
- `SyncStudentContact`
- `SyncLead` (lo invoca el gateway fire-and-forget desde la ruta pública `/api/public/leads`)

### Variables de entorno (server + worker comparten)

| Variable | Origen | Valor / Notas |
|---|---|---|
| `GRPC_PORT` | env (def `:50054`) | `:50054` |
| `WEBHOOK_HTTP_PORT` | env (def `:8080`) | solo el server lo escucha |
| `HUBSPOT_ENVIRONMENT` | values | `prod` |
| `HUBSPOT_API_TOKENS` | KV secret | `hubspot-service-api-tokens` — **CSV** de Private App Tokens (fuente canónica) |
| `HUBSPOT_API_TOKEN` | env (fallback) | singular, legacy — pool de 1 si `_TOKENS` vacío |
| `HUBSPOT_OTP_WEBHOOK_TOKEN` | KV secret | `hubspot-service-otp-webhook-token` |
| `HUBSPOT_OTP_WEBHOOK_TRIGGER_ID` | values (def `9013951`) | id del Workflow trigger |
| `HUBSPOT_CO_KEY_ID` / `HUBSPOT_CO_ASESOR_ID` / `HUBSPOT_CO_COLEGIO_ID` | values | Custom Object IDs `2-XXXXXXX` (defaults en código) |
| `HUBSPOT_RPS` | env (def `10`) | requests/seg por token |
| `HUBSPOT_ENGINE_WORKERS` | env (def `10`) | concurrencia interna del engine |
| `HUBSPOT_ENGINE_QUEUE_SIZE` | env (def `100`) | buffer del canal del engine |
| `WORKER_CONCURRENCY` | env (def `10`) | concurrencia del worker asynq |
| `REDIS_ADDR` / `REDIS_TLS` / `REDIS_PASSWORD` | `global.*` + KV | pass = `hubspot-service-redis-password` → `redis-password` |
| `REDIS_DB` | env (def `0`) | DB de asynq |
| `REDIS_DB_RATELIMIT` | env (def `1`) | DB separada para el rate limiter cross-token |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | env (def `miproposito.users`) | `miproposito.users` |

> Nota: los env `HUBSPOT_ASESOR_TEAM_ID` / `HUBSPOT_ASESOR_ROLE_ID` que aparecían
> en versiones anteriores del doc **no** los lee `config.go` actual.

### Secrets en Key Vault
- `hubspot-service-api-tokens` (CSV)
- `hubspot-service-otp-webhook-token`
- `hubspot-service-redis-password`
- `users-service-jwt-secret`

### Ingress
- `hubspot-webhook.miproposito.ucsp.edu.pe` → solo el server, puerto `8080`
  (webhooks de HubSpot Workflow). Las RPCs son ClusterIP.

---

## 5. `satisfaction-service`

**Qué hace:** encuestas de satisfacción / NPS post-examen (survey + preguntas +
respuestas, encuesta-por-key). Autónomo.

**BD:** `db_satisfaction` (Azure SQL). **No usa Redis. No llama a nadie.**

**Imagen:** `satisfaction-service:<tag>` — Dockerfile multi-target.

### RPCs que expone
`SurveyService`, `ResponseService`, `Health`.

### Variables de entorno

| Variable | Origen | Valor |
|---|---|---|
| `GRPC_PORT` | env (def `:50055`) | `:50055` |
| `SQL_*` | `global.*` + values | `db_satisfaction` |
| `SQL_PASSWORD` | KV secret | `satisfaction-service-sql-password` → `sql-password` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | env (def `miproposito.users`) | `miproposito.users` |

### Secrets en Key Vault
- `satisfaction-service-sql-password`
- `users-service-jwt-secret`

### Hooks Helm
- **pre-install/pre-upgrade:** `Job satisfaction-service-migrate`.

---

## 6. `analytics-service`

**Qué hace:** consolida datos de otros servicios para los dashboards de
asesor/colegio/estudiante/superadmin (rankings, comparativos, históricos,
pendientes). **No tiene BD propia**: consulta on-demand por gRPC y cachea en Redis.

**Dependencias gRPC (salientes, cableadas en `cmd/server/main.go`):**
`users-service`, `exams-service`, `keys-service`.
Nota: `SATISFACTION_SERVICE_ADDR` está declarado en `config.go` pero **no** se cablea
ningún cliente de satisfaction en el wiring actual (la reportería de satisfacción va
por el gateway → satisfaction-service directo, no por aquí).

**Imagen:** `analytics-service:<tag>` — single-target.

### RPCs que expone
`AnalyticsService` (GetAsesorDashboard, GetColegioDashboard, GetEstudianteDashboard,
GetColegioComparativo, GetHistorico*, GetReporteEstudiante, GetAsesorPendientes, …),
`Health`.

### Variables de entorno

| Variable | Origen | Valor |
|---|---|---|
| `GRPC_PORT` | env (def `:50056`) | `:50056` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | env (def `miproposito.users`) | `miproposito.users` |
| `USERS_SERVICE_ADDR` | derivado | `<release>-users-service:50051` |
| `EXAMS_SERVICE_ADDR` | derivado | `<release>-exams-service:50052` |
| `KEYS_SERVICE_ADDR` | derivado | `<release>-keys-service:50053` |
| `SATISFACTION_SERVICE_ADDR` | derivado | declarado en config, sin cliente cableado (ver arriba) |
| `REDIS_ADDR` / `REDIS_TLS` / `REDIS_PASSWORD` | `global.*` + KV | pass = `analytics-service-redis-password` → `redis-password` |
| `REDIS_DB` | env (def `1`) | DB `1` (separada del resto) |
| `CACHE_ENABLED` | env (def `true`) | `true` |
| `GOLANG_PROTOBUF_REGISTRATION_CONFLICT` | hardcoded | `warn` |

### Secrets en Key Vault
- `analytics-service-redis-password`
- `users-service-jwt-secret`

### Hooks Helm
Ninguno — sin BD propia.

---

## 7. `gateway`

**Qué hace:** único punto de entrada HTTP/REST público. Traduce REST → gRPC, maneja
CORS, rate-limit por IP, verifica JWT (salvo skip-list) y aplica gates de permiso por
ruta. **Aloja el asistente IA (chatbot del panel).**

**Dependencias gRPC (salientes):** TODOS los demás servicios —
users (`UserService`, `AuthService`, `SchoolService`, `LeadService`, `VisitaService`,
`PermissionGroupService`), exams (`ExamService`, `QuestionService`,
`ExamQuestionService`, `AttemptService`), keys (`KeyService`), hubspot
(`HubspotService`), satisfaction (`SurveyService`, `ResponseService`), analytics
(`AnalyticsService`). Además Redis (rate-limit) y un **LLM externo estilo OpenAI**
para el chatbot.

**Imagen:** `gateway:<tag>` — single-target.

### Endpoints notables
- `POST /api/assistant/chat` — chatbot solo-lectura del panel (`assistant.go`).
  Tool-calling sobre herramientas scopeadas por rol (nunca SQL crudo → hereda el
  scope del usuario). Charts embebidos (ApexCharts).
- Simulacro Masivo / "Prepárate": `listLeads`, `enviarAccesoLeads` en `landing.go`,
  gateados con permiso **`analytics.simulacro_masivo.read`**.
- Rutas de export: permiso `analytics.export.write`; dashboards: `analytics.dashboard.read`.

### Skip-list de JWT (rutas públicas, `cmd/server/main.go`)
```
/api/auth/login
/api/auth/refresh
/api/auth/student/request-otp
/api/auth/student/verify-otp
/api/auth/student/register-with-key
/api/auth/student/lookup-by-key
/api/keys/by-code/          (validar la key antes del OTP; el secreto es el propio code)
/api/careers
/api/public/                (encuestas PUBLICADAS anónimas + landing de leads)
/health
```
Además hay una allow-list del `MustChangePasswordGuard` (JWT con `mcp=true` solo
puede tocar `/api/users/me`, `…/me/change-password`, `/api/auth/logout`, `/health`).

### Variables de entorno

| Variable | Origen | Valor |
|---|---|---|
| `HTTP_PORT` | env (def `:8080`) | `:8080` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | env (def `miproposito.users`) | `miproposito.users` |
| `USERS_SERVICE_ADDR` … `ANALYTICS_SERVICE_ADDR` | derivado | `<release>-<svc>:5005x` (los 6 upstreams) |
| `CORS_ALLOWED_ORIGINS` | values | CSV de dominios permitidos (def `https://miproposito.ucsp.edu.pe`) |
| `RATELIMIT_ENABLED` | env (def `true`) | `true` |
| `RATELIMIT_PER_IP_PER_MIN` | env (def `600`) | `600` |
| `REDIS_ADDR` / `REDIS_TLS` / `REDIS_PASSWORD` | `global.*` + KV | pass = `gateway-redis-password` → `redis-password` |
| **Asistente IA** | | |
| `ASSISTANT_ENABLED` | env (def `true`) | habilita el chatbot |
| `LLM_BASE_URL` | env (def `https://api.openai.com/v1`) | OpenAI o `http://…-llm:11434/v1` (Ollama local) |
| `LLM_MODEL` | env (def `gpt-4o-mini`) | modelo de chat (en despliegue se sobreescribe, p.ej. `gpt-4.1`) |
| `OPENAI_API_KEY` / `LLM_API_KEY` | Secret manual | Bearer del endpoint; vacío para Ollama local |
| `GOLANG_PROTOBUF_REGISTRATION_CONFLICT` | hardcoded | `warn` |

> **Ojo (assistant):** el chart Helm del gateway **no** cablea `ASSISTANT_ENABLED` /
> `LLM_*` / `OPENAI_API_KEY`. Se inyectan manualmente (Secret `assistant-openai` en el
> cluster, análogo al caso Resend/SMTP). Con `OPENAI_API_KEY` ausente y `LLM_BASE_URL`
> apuntando a OpenAI, el chatbot falla (429/401). Ver §9.

### Secrets en Key Vault
- `gateway-redis-password`
- `users-service-jwt-secret`
- (OpenAI key NO en KV — Secret manual, §9)

### Ingress
- `api.miproposito.ucsp.edu.pe` → port `8080`. **Única** puerta pública del backend.

---

## 8. Servicios stateful / auxiliares in-cluster

Existen como subcharts pero NO son microservicios de negocio.

### `mssql-server` (solo staging)
- Activado por `global.inClusterSql=true` (default `false` en prod).
- StatefulSet con PVC, pod único. Password en `Secret mssql-server-sa` (no KV).
- Hostname interno: `mssql-server.miproposito.svc.cluster.local:1433`.

### `redis-server` (default en prod)
- Activado por `global.inClusterRedis=true` (default `true` en prod).
- StatefulSet con PVC, pod único. Password en `Secret redis-server-pwd`.
- Hostname interno: `redis-server.miproposito.svc.cluster.local:6379`.
- Reemplazable por Azure Cache for Redis cambiando `values.production.yaml`.

### `api-docs` (chart estático)
- Sirve la especificación OpenAPI (`deploy/helm/charts/api-docs`,
  `deploy/api-docs/openapi.yaml`). No es un microservicio de negocio.

### `deploy/llm/` (Ollama, opcional)
- Deployment de Ollama para servir un LLM local al chatbot (replicas típicamente `0`;
  se enciende apuntando `LLM_BASE_URL`/`LLM_MODEL` del gateway al pod).

---

## 9. Secrets manuales (fuera del umbrella)

Dos secretos hoy se crean a mano, no vienen del umbrella/KV:

### `miproposito-smtp-credentials` — OTP por SMTP (path actual)
El OTP del alumno se envía por **SMTP (Gmail)** con `OTP_SENDER=smtp`. Las
credenciales viven en un Secret manual (no en Helm):
```bash
kubectl create secret generic miproposito-smtp-credentials \
  -n miproposito \
  --from-literal=smtp-username='tu-cuenta@gmail.com' \
  --from-literal=smtp-password='<App Password de Gmail>'
```
El resto de la config SMTP (`SMTP_HOST`, `SMTP_PORT`, `SMTP_FROM`) va por values del
chart de users-service (`otp.smtp.*`). **Pendiente:** mover a Key Vault + SPC cuando
UCSP verifique un dominio corporativo de envío.

### `assistant-openai` — API key del chatbot
El gateway necesita `OPENAI_API_KEY` para el asistente. El chart no lo cablea; se
inyecta manualmente (Secret) y se monta como env `OPENAI_API_KEY` en el gateway.
**Pendiente:** integrarlo al umbrella como los demás secrets.

> Path legacy: `resend-api-key` / `miproposito-resend-credentials` ya **no** se usa
> (config de users-service retiró Resend). El Deployment aún lo referencia como env
> inerte; se limpiará.

---

## 10. Resumen accionable para el cliente

Checklist de "qué secrets crear en Key Vault antes del primer deploy":

```
Key Vault: miproposito-kv (nombre puede variar; viene del Bicep)

# Passwords SQL (una por servicio con BD)
users-service-sql-password
exams-service-sql-password
keys-service-sql-password
satisfaction-service-sql-password

# Passwords Redis (una por servicio que use Redis)
users-service-redis-password
gateway-redis-password
hubspot-service-redis-password
analytics-service-redis-password

# JWT (único, compartido por los 7)
users-service-jwt-secret               # 64 chars random recomendado

# HubSpot
hubspot-service-api-tokens             # CSV de Private App Tokens
hubspot-service-otp-webhook-token      # del Workflow trigger
```
Total: **10 secretos** en Key Vault.

Y **2 Secrets manuales** en el cluster (fuera del umbrella, §9):
```
miproposito-smtp-credentials   # smtp-username + smtp-password (OTP por Gmail)
assistant-openai               # OPENAI_API_KEY (chatbot del gateway)
```

---

## 11. Cómo verificar en vivo qué env vars está leyendo un servicio

```bash
# Ver TODAS las env vars (incluyendo las que apuntan a secrets)
kubectl get deployment -n miproposito miproposito-<servicio> \
  -o jsonpath='{.spec.template.spec.containers[0].env}' | jq .

# Ver el contenido decodificado del Secret del servicio
kubectl get secret -n miproposito miproposito-<servicio>-kv \
  -o jsonpath='{.data}' | jq 'map_values(@base64d)'

# Ver si el SecretProviderClass está montando del KV correcto
kubectl get spc -n miproposito miproposito-<servicio>-spc -o yaml
```

Si una env var apunta a un secret que no existe, el pod queda `ContainerCreating`
con `MountVolume.SetUp failed`. Eso indica: (a) el secret no se sincronizó del KV
(revisar Workload Identity), o (b) el `objectName` en el SPC no coincide con el
nombre real en KV.

Cada servicio además loguea su config efectiva al arrancar (línea `[config] …` en
`config.go`), útil para confirmar `otpsender`, upstreams y DBs de Redis sin
decodificar secrets.

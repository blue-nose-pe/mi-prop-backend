# Dependencias por microservicio — referencia operativa

> Para cada microservicio: qué necesita corriendo, qué env vars consume, qué
> secrets monta del Key Vault, y dónde se configura cada cosa. La forma
> general del despliegue (umbrella, CSI driver, ingress) vive en
> [`../deploy/helm/miproposito/ARCHITECTURE.md`](../deploy/helm/miproposito/ARCHITECTURE.md).
> La operación cotidiana (cómo deployar, rotar, debuggear) vive en
> [`../GUIA_INSTALACION_CLIENTE.md`](../GUIA_INSTALACION_CLIENTE.md).

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
│  users-service-jwt-secret  (compartido por todos)                   │
│  hubspot-service-api-tokens          hubspot-service-otp-webhook-token│
│  resend-api-key  (en Secret aparte: miproposito-resend-credentials) │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼   (Workload Identity + CSI driver)
┌─────────────────────────────────────────────────────────────────────┐
│                            AKS                                       │
│                                                                      │
│   users-service ─────┐                            ┌─→ HubSpot API   │
│   exams-service ─────┤                            ├─→ Resend API    │
│   keys-service ──────┼─→ Azure SQL (4 BDs)        │                  │
│   satisfaction-service                                                │
│                                                                      │
│   analytics-service ──→ users + exams + keys + satisfaction (gRPC)   │
│   hubspot-service ────→ Redis (asynq queue) + Azure SQL (audit)      │
│   gateway ────────────→ todos (gRPC) + Redis (rate-limit)            │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Convenciones (válidas para los 7 servicios)

| Concepto | Cómo se materializa |
|---|---|
| **Puerto interno** | `GRPC_PORT=:50051..50056` (gateway usa `HTTP_PORT=:8080`) |
| **JWT secret** | Compartido — todos leen `users-service-jwt-secret` del KV con alias `jwt-secret` |
| **JWT issuer** | `miproposito.users` (hardcodeado, en todos los Deployments) |
| **Imagen Docker** | `<registry>/<service>:<tag>`, registry y tag vienen de `global.*` |
| **Secret en cluster** | `miproposito-<service>-kv` (Opaque, sincronizado desde KV por SPC) |
| **Health check** | gRPC `/grpc.health.v1.Health/Check` — los readiness/liveness probes apuntan a esto |
| **Logs** | stdout en formato `2026/05/22 02:15:19 [correlation-id] METHOD path duration result` |

---

## 1. `users-service`

**Qué hace:** identidad y autorización. Login con password, refresh tokens,
OTP para estudiantes (vía Resend), permission groups, audit log. Es la fuente
de verdad de quién es quién en el sistema.

**Necesita corriendo:** Azure SQL (`db_users`), Redis (cache de user lookup),
`hubspot-service` (para sincronizar contactos student a HubSpot best-effort).

**Imagen:** `users-service:<tag>` (binario `server`) — Dockerfile multi-target,
también produce `users-service-migrate` para el Job de migración.

### Variables de entorno (deployment.yaml + values.yaml)

| Variable | Origen | Valor / Notas |
|---|---|---|
| `GRPC_PORT` | hardcoded | `:50051` |
| `SQL_SERVER` | `global.sqlServer` | Azure SQL FQDN (prod) o `mssql-server.miproposito.svc.cluster.local` (staging) |
| `SQL_PORT` | `global.sqlPort` | `1433` |
| `SQL_DATABASE` | values | `db_users` |
| `SQL_USER` | values | `miprop_admin` |
| `SQL_PASSWORD` | KV secret | `users-service-sql-password` → alias `sql-password` |
| `SQL_ENCRYPT` | `global.sqlEncrypt` | `true` en prod (Azure SQL exige TLS) |
| `SQL_TRUST_SERVER_CERT` | `global.sqlTrustServerCert` | `false` en prod |
| `REDIS_ADDR` | derivado de `global.redisHost:Port` | `redis-server...:6379` |
| `REDIS_PASSWORD` | KV secret | `users-service-redis-password` → alias `redis-password` |
| `REDIS_TLS` | `global.redisTls` | `false` in-cluster, `true` Azure Cache |
| `REDIS_DB` | values | `0` |
| `CACHE_TTL` | values | `5m` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → alias `jwt-secret` |
| `JWT_ISSUER` | hardcoded | `miproposito.users` |
| `JWT_ACCESS_TTL` | values | `12h` |
| `JWT_REFRESH_TTL` | values | `168h` (7 días) |
| `BCRYPT_COST` | values | `12` en prod, 10 en dev |
| `HUBSPOT_SERVICE_ADDR` | derivado | `miproposito-hubspot-service:50054` |
| `OTP_SENDER` | values | `resend` (default) o `hubspot` (path antiguo via Workflow) |
| `RESEND_FROM` | values | `"Mi Proposito UCSP <onboarding@resend.dev>"` — para prod, cambiar a `noreply@<dominio-verificado>` |
| `RESEND_REPLY_TO` | values | opcional |
| `RESEND_API_KEY` | KV secret | `resend-api-key` (en secret `miproposito-resend-credentials`, ver §9) |
| `GOLANG_PROTOBUF_REGISTRATION_CONFLICT` | hardcoded | `warn` (silencia warning de protos compartidos) |

### Secrets que necesita en Key Vault
- `users-service-sql-password`
- `users-service-redis-password`
- `users-service-jwt-secret` (este lo leen también los otros 6 servicios)
- `resend-api-key` (separado, ver §9)

### Hooks Helm
- **pre-install/pre-upgrade:** `Job users-service-migrate` — corre las
  migraciones T-SQL contra `db_users`.
- **post-install:** `Job users-service-bootstrap` — crea el primer superadmin
  si no existe. Lee email de `bootstrap.email` en values.yaml; genera password
  random y la guarda en `Secret miproposito-users-service-superadmin`. El
  cliente la recupera con:
  ```bash
  kubectl get secret miproposito-users-service-superadmin -n miproposito \
    -o jsonpath='{.data.password}' | base64 -d
  ```

---

## 2. `exams-service`

**Qué hace:** catálogo de exámenes, preguntas, opciones; attempts (intentos
del estudiante), respuestas, scoring. Gate atómico de aforo via keys-service
antes de crear un attempt.

**Necesita corriendo:** Azure SQL (`db_exams`), `keys-service` (para
ValidateKey + IncrementUsage). NO usa Redis.

**Imagen:** `exams-service:<tag>` — Dockerfile multi-target (server + migrate).

### Variables de entorno

| Variable | Origen | Valor |
|---|---|---|
| `GRPC_PORT` | hardcoded | `:50052` |
| `SQL_SERVER` / `SQL_PORT` / `SQL_ENCRYPT` / `SQL_TRUST_SERVER_CERT` | `global.*` | igual que users |
| `SQL_DATABASE` | values | `db_exams` |
| `SQL_USER` | values | `miprop_admin` |
| `SQL_PASSWORD` | KV secret | `exams-service-sql-password` → `sql-password` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | hardcoded | `miproposito.users` |
| `KEYS_SERVICE_ADDR` | derivado | `miproposito-keys-service:50053` |
| `GOLANG_PROTOBUF_REGISTRATION_CONFLICT` | hardcoded | `warn` |

### Secrets que necesita en Key Vault
- `exams-service-sql-password`
- `users-service-jwt-secret`

### Hooks Helm
- **pre-install/pre-upgrade:** `Job exams-service-migrate`.

---

## 3. `keys-service`

**Qué hace:** códigos de acceso (keys) que los asesores entregan a estudiantes.
Validación, vigencia, aforo atómico. v0.16+ permite atar 1 key a un exam_id
puntual.

**Necesita corriendo:** Azure SQL (`db_keys`). NO usa Redis. NO depende de
otros servicios.

**Imagen:** `keys-service:<tag>` — Dockerfile multi-target (server + migrate).

### Variables de entorno

| Variable | Origen | Valor |
|---|---|---|
| `GRPC_PORT` | hardcoded | `:50053` |
| `SQL_*` | `global.*` + values | `db_keys` |
| `SQL_PASSWORD` | KV secret | `keys-service-sql-password` → `sql-password` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | hardcoded | `miproposito.users` |

### Secrets que necesita en Key Vault
- `keys-service-sql-password`
- `users-service-jwt-secret`

### Hooks Helm
- **pre-install/pre-upgrade:** `Job keys-service-migrate`.

---

## 4. `hubspot-service`

**Qué hace:** sincronización con HubSpot CRM — upsert de contactos, custom
objects (key, asesor, colegio), envío de OTP vía Workflow (path antiguo,
fallback), webhooks entrantes. Tiene **2 binarios separados** (`server` y
`worker`), por eso son **2 Deployments**.

**Necesita corriendo:** Redis (asynq queue para jobs async), `users-service`
(persistir record_id del contacto), API externa de HubSpot.

**Imagen:** `hubspot-service:<tag>` (server) + `hubspot-service:<tag>` con
command override (worker). Dockerfile multi-target; recordar `--target server`
o `--target worker`.

### Variables de entorno (server + worker comparten todas)

| Variable | Origen | Valor / Notas |
|---|---|---|
| `GRPC_PORT` | hardcoded | `0.0.0.0:50054` |
| `WEBHOOK_HTTP_PORT` | hardcoded | `:8080` — solo server lo escucha |
| `HUBSPOT_ENVIRONMENT` | values | `prod` |
| `HUBSPOT_API_TOKENS` | KV secret | `hubspot-service-api-tokens` — CSV de Private App Tokens (más tokens = más rps global) |
| `HUBSPOT_OTP_WEBHOOK_TOKEN` | KV secret | `hubspot-service-otp-webhook-token` |
| `HUBSPOT_OTP_WEBHOOK_TRIGGER_ID` | values | id numérico del Workflow trigger (HubSpot UI) |
| `HUBSPOT_CO_KEY_ID` | values | `2-XXXXXXX` — Custom Object ID de "Key" |
| `HUBSPOT_CO_ASESOR_ID` | values | `2-XXXXXXX` — Custom Object ID de "Asesor" |
| `HUBSPOT_CO_COLEGIO_ID` | values | `2-XXXXXXX` — Custom Object ID de "Colegio" |
| `HUBSPOT_ASESOR_TEAM_ID` | values | id numérico del team al que invitar asesores como users HubSpot |
| `HUBSPOT_ASESOR_ROLE_ID` | values | id numérico del rol HubSpot |
| `HUBSPOT_RPS` | values | `10` — requests per second por token |
| `HUBSPOT_ENGINE_WORKERS` | values | `10` — concurrencia interna del engine |
| `HUBSPOT_ENGINE_QUEUE_SIZE` | values | `100` — buffer de jobs en memoria |
| `REDIS_HOST` / `REDIS_PORT` / `REDIS_ADDR` / `REDIS_TLS` / `REDIS_DB` | `global.*` | DB `0` (asynq queue) |
| `REDIS_DB_RATELIMIT` | values | `1` — DB separada para el rate limiter cross-token |
| `REDIS_PASSWORD` | KV secret | `hubspot-service-redis-password` → `redis-password` |
| `USERS_SERVICE_GRPC` | derivado | `miproposito-users-service:50051` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | hardcoded | `miproposito.users` |

### Secrets que necesita en Key Vault
- `hubspot-service-api-tokens` (CSV)
- `hubspot-service-otp-webhook-token`
- `hubspot-service-redis-password`
- `users-service-jwt-secret`

### Métodos públicos (no requieren JWT)
Algunos RPCs son intra-cluster y los invoca el flow anónimo del estudiante.
Están en el JWT skip list de hubspot-service (`cmd/server/main.go`):
- `SendOTP`
- `UpsertContact`
- `SyncStudentContact`

> Esto NO los expone públicamente — el ingress de hubspot-service solo tiene
> el webhook HTTP. Las RPCs son ClusterIP. "Sin JWT" significa que callers
> internos (users-service durante registro anónimo) pueden invocarlas.

### Ingress
- `hubspot-webhook.miproposito.ucsp.edu.pe` → solo el server, puerto 8080,
  recibe webhooks de HubSpot Workflow.

---

## 5. `satisfaction-service`

**Qué hace:** encuestas de satisfacción NPS. Bastante autónomo.

**Necesita corriendo:** Azure SQL (`db_satisfaction`). NO usa Redis. NO depende
de otros servicios.

**Imagen:** `satisfaction-service:<tag>` — Dockerfile multi-target.

### Variables de entorno

| Variable | Origen | Valor |
|---|---|---|
| `GRPC_PORT` | hardcoded | `:50055` |
| `SQL_*` | `global.*` + values | `db_satisfaction` |
| `SQL_PASSWORD` | KV secret | `satisfaction-service-sql-password` → `sql-password` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | hardcoded | `miproposito.users` |

### Secrets que necesita en Key Vault
- `satisfaction-service-sql-password`
- `users-service-jwt-secret`

### Hooks Helm
- **pre-install/pre-upgrade:** `Job satisfaction-service-migrate`.

---

## 6. `analytics-service`

**Qué hace:** consolida datos de los otros servicios para los dashboards de
asesor/colegio/superadmin. NO tiene BD propia — todo se consulta on-demand
via gRPC y se cachea en Redis.

**Necesita corriendo:** Redis (cache), `users-service`, `exams-service`,
`keys-service`, `satisfaction-service` (todos via gRPC).

**Imagen:** `analytics-service:<tag>` — single-target.

### Variables de entorno

| Variable | Origen | Valor |
|---|---|---|
| `GRPC_PORT` | hardcoded | `:50056` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | hardcoded | `miproposito.users` |
| `USERS_SERVICE_ADDR` | derivado | `miproposito-users-service:50051` |
| `EXAMS_SERVICE_ADDR` | derivado | `miproposito-exams-service:50052` |
| `KEYS_SERVICE_ADDR` | derivado | `miproposito-keys-service:50053` |
| `SATISFACTION_SERVICE_ADDR` | derivado | `miproposito-satisfaction-service:50055` |
| `REDIS_ADDR` / `REDIS_TLS` | `global.*` | DB `1` (separada del resto) |
| `REDIS_PASSWORD` | KV secret | `analytics-service-redis-password` → `redis-password` |
| `REDIS_DB` | values | `1` |
| `CACHE_ENABLED` | values | `true` |
| `GOLANG_PROTOBUF_REGISTRATION_CONFLICT` | hardcoded | `warn` |

### Secrets que necesita en Key Vault
- `analytics-service-redis-password`
- `users-service-jwt-secret`

### Hooks Helm
Ninguno — sin BD propia.

---

## 7. `gateway`

**Qué hace:** punto de entrada HTTP/REST público. Traduce REST → gRPC,
maneja CORS, rate-limit por IP, verifica JWT (excepto rutas públicas:
login, refresh, request-otp, verify-otp, register-with-key, lookup-by-key,
keys/by-code, careers, health).

**Necesita corriendo:** TODOS los demás servicios (via gRPC), Redis
(rate-limit), Workload Identity para leer KV.

**Imagen:** `gateway:<tag>` — single-target.

### Variables de entorno

| Variable | Origen | Valor |
|---|---|---|
| `HTTP_PORT` | hardcoded | `:8080` |
| `JWT_SECRET` | KV secret | `users-service-jwt-secret` → `jwt-secret` |
| `JWT_ISSUER` | hardcoded | `miproposito.users` |
| `USERS_SERVICE_ADDR` | derivado | `miproposito-users-service:50051` |
| `EXAMS_SERVICE_ADDR` | derivado | `miproposito-exams-service:50052` |
| `KEYS_SERVICE_ADDR` | derivado | `miproposito-keys-service:50053` |
| `HUBSPOT_SERVICE_ADDR` | derivado | `miproposito-hubspot-service:50054` |
| `SATISFACTION_SERVICE_ADDR` | derivado | `miproposito-satisfaction-service:50055` |
| `ANALYTICS_SERVICE_ADDR` | derivado | `miproposito-analytics-service:50056` |
| `CORS_ALLOWED_ORIGINS` | values | CSV de dominios permitidos |
| `RATELIMIT_ENABLED` | values | `true` |
| `RATELIMIT_PER_IP_PER_MIN` | values | `600` |
| `REDIS_ADDR` / `REDIS_TLS` | `global.*` | DB `0` (rate limit) |
| `REDIS_PASSWORD` | KV secret | `gateway-redis-password` → `redis-password` |
| `GOLANG_PROTOBUF_REGISTRATION_CONFLICT` | hardcoded | `warn` |

### Secrets que necesita en Key Vault
- `gateway-redis-password`
- `users-service-jwt-secret`

### Ingress
- `api.miproposito.ucsp.edu.pe` → port 8080. Es la **única** puerta pública
  del backend.

---

## 8. Servicios stateful in-cluster

Existen como subcharts pero NO son microservicios. Solo se activan según
flags globales.

### `mssql-server` (solo staging)
- Activado por `global.inClusterSql=true` (default `false` en prod).
- Pod único, StatefulSet con PVC.
- Password en `Secret mssql-server-sa` (no en KV — se genera al instalar).
- Hostname interno: `mssql-server.miproposito.svc.cluster.local:1433`.

### `redis-server` (default en prod)
- Activado por `global.inClusterRedis=true` (default `true` en prod).
- Pod único, StatefulSet con PVC.
- Password en `Secret redis-server-pwd`.
- Hostname interno: `redis-server.miproposito.svc.cluster.local:6379`.
- Se puede reemplazar por Azure Cache for Redis cambiando `values.production.yaml`
  (ver §6.1 de [`ARCHITECTURE.md`](../deploy/helm/miproposito/ARCHITECTURE.md)).

---

## 9. Secret `miproposito-resend-credentials` (fuera del umbrella)

Resend HTTP es el path actual de envío de OTP (bypass del Workflow de HubSpot
que UCSP no termina de configurar — ver `README.md → Envío del OTP`).

**No está en el umbrella todavía.** Se crea manualmente:

```bash
kubectl create secret generic miproposito-resend-credentials \
  -n miproposito \
  --from-literal=resend-api-key='re_XXXXXXXXX'
```

Y el Deployment de users-service lo monta como env:
```yaml
- name: RESEND_API_KEY
  valueFrom:
    secretKeyRef:
      name: miproposito-resend-credentials
      key: resend-api-key
```

**Pendiente:** mover esto a Key Vault y montarlo via SPC como los otros
secrets, una vez que UCSP verifique su dominio en Resend y pasemos a from
address corporativa.

---

## 10. Resumen accionable para el cliente

Si DevOps de UCSP necesita una **checklist plana** de "qué secrets crear en
KV antes del primer deploy":

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

# JWT (único, compartido)
users-service-jwt-secret               # 64 chars random recomendado

# HubSpot
hubspot-service-api-tokens             # CSV de Private App Tokens
hubspot-service-otp-webhook-token      # del Workflow trigger

# Resend (no en KV todavía, ver §9)
# kubectl create secret miproposito-resend-credentials --from-literal=resend-api-key=re_...
```

Total: **10 secretos** en Key Vault + 1 Secret manual para Resend.

---

## 11. Cómo verificar en vivo qué env vars está leyendo un servicio

Útil para debugging cuando algo se desconfigura:

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

Si una env var apunta a un secret que no existe, el pod queda
`ContainerCreating` con error `MountVolume.SetUp failed`. Eso indica:
- (a) el secret no se sincronizó del KV (revisar Workload Identity), o
- (b) el `objectName` en el SPC no coincide con el nombre real en KV.

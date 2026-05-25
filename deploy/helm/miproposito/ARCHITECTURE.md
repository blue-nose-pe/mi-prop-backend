# Arquitectura del umbrella chart — `miproposito`

> Este documento es la **fuente de verdad del diseño** del despliegue de
> Mi Propósito 2.0 sobre AKS. Explica QUÉ se despliega, CÓMO se compone, y
> sobre todo POR QUÉ se tomaron las decisiones que se ven en `Chart.yaml`,
> `values.yaml` y los subcharts. La operación día-a-día (cómo instalar,
> rotar secrets, hacer rollback) vive en
> [`../../../GUIA_INSTALACION_CLIENTE.md`](../../../GUIA_INSTALACION_CLIENTE.md).

---

## 1. Forma general

```
deploy/helm/
├── charts/                          ← subcharts (1 por servicio o dependencia)
│   ├── users-service/
│   ├── exams-service/
│   ├── keys-service/
│   ├── hubspot-service/
│   ├── satisfaction-service/
│   ├── analytics-service/
│   ├── gateway/
│   ├── mssql-server/                ← solo activo en staging
│   ├── redis-server/                ← in-cluster en prod (default)
│   └── api-docs/                    ← separado del umbrella (Helm release aparte)
│
└── miproposito/                     ← umbrella
    ├── Chart.yaml                   ← declara los 10 deps + condiciones
    ├── Chart.lock
    ├── values.yaml                  ← defaults compartidos
    ├── values.staging.yaml          ← override: SQL+Redis in-cluster
    ├── values.production.yaml       ← override: Azure SQL + Redis in-cluster
    └── templates/                   ← (vacío — el umbrella no tiene templates propios)
```

Un solo `helm upgrade --install miproposito ./deploy/helm/miproposito ...` levanta
todo. Cada subchart se enciende/apaga con un flag `<service>.enabled` (ver
`values.yaml` → `usersService.enabled`, `examsService.enabled`, etc.).

---

## 2. Por qué umbrella (y no N charts independientes)

La pregunta razonable es: si cada servicio es autónomo y se buildea por
separado en ACR, ¿para qué unificarlos bajo un único chart?

**Tres razones concretas:**

1. **Una sola versión coherente.** En P1 cada herramienta deployaba
   independientemente y aparecían combinaciones inviables (front v3, back v2,
   keys v4). Con umbrella, `Chart.yaml` clava `version: 0.1.0` para TODOS los
   subcharts simultáneamente: el cliente nunca termina con users-service v0.3
   hablando con exams-service v0.1. `helm upgrade` aplica el set entero o nada.

2. **Switches globales.** `global.inClusterSql`, `global.inClusterRedis`,
   `global.keyVault.*`, `global.imageRegistry` se definen UNA vez en
   `values.yaml` y los subcharts los heredan via `.Values.global.*`. Sin
   umbrella, tendrías que repetir 10 veces el host de SQL en cada `values.yaml`.
   Esto se ve en `_helpers.tpl` de cada subchart cuando arman el `SQL_SERVER`
   env var.

3. **Order de instalación + hooks transversales.** Los charts de migración
   (`job-migrate.yaml` en users/exams/keys/satisfaction) corren como
   `pre-install,pre-upgrade` hooks. El umbrella es quien fija el orden
   garantizando que MSSQL esté listo antes que las migrations, y las migrations
   antes que los Deployments. En charts sueltos eso es manual y frágil.

**Trade-off conocido:** `helm upgrade` re-evalúa los 10 charts cada vez, así
que un cambio en un subchart hace que Helm chequee si los otros 9 también
necesitan upgrade. En la práctica el delta es nulo (Helm es idempotente) pero
el `helm diff` se vuelve un poco verboso. Aceptable.

---

## 3. Charts en `Chart.yaml` — qué corre cada uno

| Subchart | Tipo | Cuándo corre | Cómo se prende |
|---|---|---|---|
| `mssql-server` | Pod stateful único | Solo staging (`global.inClusterSql=true`) | `mssqlServer.enabled` |
| `redis-server` | Pod stateful único | Prod + staging (default) | `redisServer.enabled` |
| `users-service` | Deployment + Service + HPA + 2 Jobs (migrate, bootstrap) + SPC | Siempre | `usersService.enabled` |
| `exams-service` | Deployment + Service + HPA + Job (migrate) + SPC | Siempre | `examsService.enabled` |
| `keys-service` | Deployment + Service + HPA + Job (migrate) + SPC | Siempre | `keysService.enabled` |
| `hubspot-service` | **2 Deployments** (server + worker) + Service + Ingress (webhook) + SPC | Siempre | `hubspotService.enabled` |
| `satisfaction-service` | Deployment + Service + HPA + Job (migrate) + SPC | Siempre | `satisfactionService.enabled` |
| `analytics-service` | Deployment + Service + HPA + SPC (sin BD propia) | Siempre | `analyticsService.enabled` |
| `gateway` | Deployment + Service + Ingress (API público) + SPC | Siempre | `gateway.enabled` |

> **Nota:** `api-docs` es un Helm release **separado** del umbrella (Swagger UI
> standalone, no necesita versionar lockstep con los servicios). No vive bajo
> `Chart.yaml`. Se deploya con `helm install api-docs ./deploy/helm/charts/api-docs ...`.

### 3.1 Por qué `hubspot-service` tiene 2 Deployments

El cliente puede confundirse al ver `miproposito-hubspot-service-*` y
`miproposito-hubspot-service-worker-*` corriendo en paralelo. Son la misma
imagen Docker pero con dos `command` distintos:

- **server** (entrypoint `/server`): expone gRPC `:50054` y HTTP webhook `:8080`.
  Atiende llamadas síncronas (UpsertContact, SendOTP) y eventos webhook de
  HubSpot.
- **worker** (entrypoint `/worker`): consume jobs encolados en Redis (asynq).
  Procesa bulk-syncs lentos sin bloquear al caller.

Patrón clásico de "API + background worker, mismo binario distinto comando".
Permite escalar replicas independientemente — más workers para una migración
pesada sin tocar la latencia del server.

> **Gotcha de deploy:** el `Dockerfile` de hubspot-service es **multi-target**
> (`AS server` y `AS worker`). Al builder con `az acr build`, hay que pasar
> `--target server` explícito — el default agarra el último `FROM` y el pod
> arranca el binario equivocado.

---

## 4. Secrets: Key Vault → CSI Driver → Kubernetes Secret → env

Patrón end-to-end, idéntico en todos los servicios:

```
Azure Key Vault                CSI Driver               Kubernetes
┌────────────────────┐         ┌──────────────┐         ┌─────────────────────────┐
│ users-service-     │         │ SPC           │         │ Secret:                 │
│   sql-password     │ ──────► │ (per-service) │ ──────► │ miproposito-users-      │
│                    │         │               │         │   service-kv            │
│ users-service-     │ ──────► │               │ ──────► │   ├─ sql-password       │
│   jwt-secret       │         │               │         │   ├─ jwt-secret         │
│                    │         │               │         │   └─ redis-password     │
│ ...                │         │               │         │                         │
└────────────────────┘         └──────────────┘         └────────┬────────────────┘
        ▲                                                         │
        │                                                         │ secretKeyRef
        │ Workload Identity                                       ▼
        │ (clientID en SPC)                                ┌────────────────┐
        └──────────────────────────────────────────────────┤ Deployment env │
                                                          │  SQL_PASSWORD  │
                                                          │  JWT_SECRET    │
                                                          │  REDIS_PASSWORD│
                                                          └────────────────┘
```

**Pieza por pieza:**

1. **Key Vault** (`{{ .Values.global.keyVault.name }}`): fuente de verdad. Naming
   convention `<service>-<type>` (`users-service-sql-password`,
   `hubspot-service-api-tokens`).

2. **SecretProviderClass** (`templates/secret-provider-class.yaml` en cada
   subchart): le dice al CSI driver qué `objectName` montar desde KV. Usa
   `objectAlias` para renombrar al estilo k8s (`users-service-sql-password` →
   `sql-password`).

3. **Workload Identity**: `clientID: {{ .Values.global.keyVault.userAssignedIdentityID }}`.
   El pod asume esta identidad managed para leer el KV sin password ni secret
   compartido. Configurada por el Bicep en la fase 2 de la guía de instalación.

4. **secretObjects:** sincroniza los blobs montados como volume hacia un
   `Secret` real de k8s. Esto permite usar `secretKeyRef` en el Deployment env
   en vez de un `volumeMount`, que es más limpio.

5. **El Deployment** lee de `miproposito-<service>-kv` con `secretKeyRef`. Ver
   por ejemplo `users-service/templates/deployment.yaml`.

> **Por qué este patrón vs. env vars directas o k8s Secrets puros:** rotación.
> Si UCSP rota la SQL password en el portal Azure, basta con escribir el nuevo
> valor en KV; el CSI driver lo refresca (default cada 2 min) y los pods lo
> ven en el próximo restart. Sin KV habría que `kubectl edit secret` a mano y
> mantener el secret commiteado fuera del repo.

### 4.1 El JWT secret se comparte

`users-service-jwt-secret` lo monta TODO servicio (no solo users). Es el
mismo secret porque el gateway lo emite y los demás servicios lo validan
(HS256 symmetric). Si lo rotás, hay que actualizar TODOS los SecretProviderClass
a la vez (y por eso hay un solo `objectName: users-service-jwt-secret` en cada
subchart, no uno per-service).

---

## 5. Networking y dominios

```
                Internet
                    │
                    ▼
       ┌──────────────────────────┐
       │ nginx-ingress LoadBalancer│
       │       20.252.12.84        │
       └─────────┬─────────────────┘
                 │
   ┌─────────────┼──────────────────┐
   │             │                  │
   ▼             ▼                  ▼
api.miproposito.ucsp.edu.pe   hubspot-webhook.miproposito.ucsp.edu.pe
   │             │                  │
   ▼             ▼                  ▼
gateway         gateway        hubspot-service (port 8080)
(8080)         (8080)            (HTTP, no gRPC)
```

- **Ingress público:** declarado en `gateway/templates/ingress.yaml`. Es la
  ÚNICA puerta exterior del backend. Todos los `/api/*` entran acá.
- **Ingress de webhooks:** declarado en `hubspot-service/templates/ingress-webhook.yaml`.
  Subdominio aparte para que HubSpot dispare eventos a un endpoint que no
  comparte rate-limit ni CORS con el API público.
- **gRPC interno:** los servicios se hablan por `ClusterIP` (`<release>-<service>:<port>`),
  nunca pasan por el ingress. Los addrs están hardcodeados como env vars en el
  Deployment (`USERS_SERVICE_ADDR`, etc.).
- **TLS:** cert-manager genera certs Let's Encrypt automáticos. Ver
  `cert-manager` + `Issuer` en el cluster (no es parte del umbrella, se instala
  como prerequisito en Fase 2 de la guía).

---

## 6. Migraciones de DB

Cada servicio que tiene su BD propia (`users`, `exams`, `keys`, `satisfaction`)
trae un `job-migrate.yaml` declarado como `helm.sh/hook: pre-install,pre-upgrade`.

```
helm upgrade
   │
   ├─► (1) Crea/actualiza SPCs (sync de secrets) ── hook: pre-install
   ├─► (2) Corre Jobs migrate (4 paralelos)        ── hook: pre-install, pre-upgrade
   │       └─ migra T-SQL con `users-service-migrate` (binario aparte)
   ├─► (3) Aplica Deployments + Services + HPAs   ── recurso normal
   └─► (4) Corre Job bootstrap (users-service)    ── hook: post-install
           └─ crea el primer superadmin si no existe
```

> **Gotcha de migraciones:** los `Job` de migrate también vienen del Dockerfile
> multi-target. Mismo cuidado que con hubspot-service: el image que usa el Job
> es `users-service-migrate:<tag>`, NO `users-service:<tag>`. Si pisás eso en
> el pipeline, los pods de los Deployments arrancan con el binario migrate y
> entran en CrashLoop.

> **Gotcha de SQL:** los `filtered indexes` (CREATE INDEX ... WHERE ...) exigen
> `SET QUOTED_IDENTIFIER ON; SET ANSI_NULLS ON;` al inicio del script. sqlcmd
> los setea OFF por default — toda migración que tenga filtered index DEBE
> empezar con esa línea. Ver `keys_service/db/migrations/004_key_exam_id.sql`
> como ejemplo.

---

## 7. Flujo completo de un deploy

```
git push origin main
   │
   ▼
Azure DevOps Pipeline (azure-pipelines.yaml)
   │
   ├── Stage 1: Build (paralelo)
   │     ├─ az acr build --target server users-service
   │     ├─ az acr build --target migrate users-service-migrate
   │     ├─ az acr build --target server exams-service
   │     ├─ az acr build keys-service        ← Dockerfile single-target
   │     ├─ az acr build --target server hubspot-service
   │     ├─ az acr build --target worker hubspot-service-worker
   │     ├─ az acr build satisfaction-service
   │     ├─ az acr build analytics-service
   │     └─ az acr build gateway
   │
   ├── Stage 2: Deploy (manual approval para production)
   │     └─ helm upgrade --install miproposito ./deploy/helm/miproposito
   │           -n miproposito --create-namespace
   │           -f values.production.yaml
   │           --set global.imageTag=$(Build.BuildId)
   │
   └── Stage 3: Smoke test
         └─ curl https://api.miproposito.ucsp.edu.pe/health → 200
```

El cliente solo necesita `git push`. El pipeline corre el resto.

---

## 8. Decisiones operativas: por qué así y no de otra forma

### 8.1 Redis in-cluster vs. Azure Cache for Redis

Default = **in-cluster** (`redis-server` subchart). El ahorro vs. Azure Cache
es ~$15/mes en SKU básico; suficiente porque Redis acá es **solo cache no
crítico** (lookup de users + asynq queue para HubSpot worker). Si Redis muere,
el sistema sigue funcionando con latencia degradada y los workers reintentan.

El cliente puede activar Azure Cache gestionado re-corriendo el Bicep con
`-EnableAzureRedis`. Se sobreescribe en `values.production.yaml`:
```yaml
inClusterRedis: false
redisHost: <output>.redis.cache.windows.net
redisSslPort: 6380
redisTls: "true"
redisServer: { enabled: false }
```

### 8.2 Una BD por servicio (no una BD compartida)

`db_users`, `db_exams`, `db_keys`, `db_satisfaction`. Sin FK físicas
cross-database porque Azure SQL no lo soporta. Las referencias son **lógicas**
y se validan a nivel servicio (ej. `exams_service.StartAttempt` consulta
`keys_service.ValidateKey` por gRPC antes de crear el attempt).

**Por qué:** aísla rollouts de migraciones. Cambiar el schema de `db_keys` no
toca `db_exams`. Y permite que cada servicio tenga su propio SQL_USER (P1
tenía un solo usuario root, P2 lo divide aunque por ahora todos usan el mismo
`miprop_admin` — preparado para granular en el futuro).

### 8.3 `replicaCount: 2` default + HPA

Todos los Deployments arrancan con 2 réplicas y un HPA que escala hasta 6 a
75% CPU (`values.yaml → defaults.autoscaling`). El 2 mínimo evita downtime
durante un rolling update. El 6 máximo es un techo conservador — el AKS de
3 nodos no aguanta más sin upgrade del SKU.

---

## 9. Gotchas y decisiones a discutir

> Esta sección es la que más vale para una reunión con un revisor externo.
> Son cosas que están como están por una razón, pero que un infra-team del
> lado UCSP probablemente quiera revisitar.

### Pendientes de hardening
- **No hay PodDisruptionBudget.** Si AKS hace un drain de nodo, los 2 replicas
  pueden bajar a 0 momentáneamente. Agregar un PDB con `minAvailable: 1` es
  trivial pero requiere tocar cada subchart.
- **No hay NetworkPolicies.** Hoy cualquier pod del namespace puede hablarle
  a cualquier otro. Una NP que solo permita `gateway → *` y `worker → externalkv`
  cerraría la superficie lateral si un pod fuera comprometido.
- **El logging va a stdout y AKS lo rota a 10MB/file.** Para retención larga
  hay que activar Container Insights o exportar a Log Analytics workspace.
  Está documentado en la guía pero no automatizado.
- **El bootstrap del superadmin sobreescribe la password si la corres a mano.**
  El Job es idempotente sobre "existe el usuario" pero si forzás la reejecución
  via `helm uninstall + install` se regenera la password. Documentar mejor.

### Decisiones que están abiertas
- ¿`replicaCount: 2` es suficiente para producción real, o el cliente espera
  3 mínimo para sobrevivir a `kubectl drain` con buffer?
- ¿El rate limit de 600 rpm/IP en el gateway es razonable para el caso de uso
  de UCSP, o hay flows masivos que necesiten más (ej. asesor cargando 200
  estudiantes a la vez via bulk upload)?
- ¿El JWT secret compartido es aceptable, o el cliente quiere uno per-servicio
  con un sidecar de rotación?
- ¿Hubspot worker debería tener su propio HPA basado en queue depth en vez de
  CPU?

---

## 10. Glosario de archivos clave

| Archivo | Para qué |
|---|---|
| [`Chart.yaml`](Chart.yaml) | Declara los 10 subcharts con sus condiciones. |
| [`values.yaml`](values.yaml) | Defaults heredados (puerto, replicas, registry, dominio, KV name). |
| [`values.production.yaml`](values.production.yaml) | Override para Azure SQL + Workload Identity real. |
| [`values.staging.yaml`](values.staging.yaml) | Override para SQL+Redis in-cluster, sin KV. |
| `charts/<service>/Chart.yaml` | Versión del subchart (siempre lockstep con umbrella). |
| `charts/<service>/values.yaml` | Defaults del servicio (puerto, recursos, flags propios). |
| `charts/<service>/templates/deployment.yaml` | Cuántos pods, qué image, qué env vars, qué probes. |
| `charts/<service>/templates/secret-provider-class.yaml` | Qué secrets de KV monta el servicio y con qué alias. |
| `charts/<service>/templates/job-migrate.yaml` | Job de migración SQL (solo servicios con BD). |
| `charts/users-service/templates/job-bootstrap.yaml` | Crea el primer superadmin post-install. |

---

## 11. Dependencias por servicio: ver `docs/SERVICES.md`

Para la lista completa de env vars, secrets y dependencias gRPC de cada
microservicio, ver [`../../../docs/SERVICES.md`](../../../docs/SERVICES.md).

Este documento es de **diseño**. Ese es la **referencia operativa**.

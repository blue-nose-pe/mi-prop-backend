# Mi Propósito 2.0 — Estado DevOps (resumen para Claude)

> Documento de contexto para que cualquier sesión nueva de Claude (u otro
> dev) entienda **dónde está parado el setup de infra/CI/CD**, qué decisiones
> se tomaron, y qué falta. Pegale este archivo entero a Claude y va a poder
> seguir trabajando sin pedir contexto adicional.

---

## TL;DR

- **Cliente**: UCSP (Universidad Católica San Pablo, Arequipa). Vendor: Blue Nose Pe. Lead dev: Pablo Pérez (`pablo.perez@bluenose.pe`).
- **Sistema**: backend Mi Propósito 2.0, **7 microservicios** (6 Go + 1 Node) que sirven 3 módulos de tests vocacionales (Vocacional, Simulacro, Hábitos) + admin.
- **Infra objetivo**: Azure (AKS + Azure SQL + Key Vault + ACR + Redis + Storage + VNet privada).
- **Repo + CI/CD**: Azure DevOps (no GitHub).
- **Estado actual** (2026-05-03):
  - ✅ Código de los 7 microservicios escrito (hexagonal + CQRS).
  - ✅ Bicep modular completo y validado contra outputs.
  - ✅ Helm umbrella chart + 7 subcharts.
  - ✅ Pipeline `azure-pipelines.yml` (test → images → deploy con manual approval).
  - ✅ Installer 1-click `deploy/install.ps1` (PowerShell).
  - ✅ Guía de instalación 5 fases en `deploy/INSTALL.md`.
  - ✅ Guía Azure DevOps en `deploy/azure-devops/README.md`.
  - 🟡 **Pendiente**: el cliente todavía no ejecutó `install.ps1` ni configuró Azure DevOps. Tampoco hicimos commit/push del trabajo reciente.

---

## Arquitectura Azure (lo que el Bicep aprovisiona)

Replica el diagrama oficial del cliente:

```
                            Internet
                                │
                                ▼
       ┌────────────────────────────────────────────────────────┐
       │  Resource Group: rg-miproposito  (region: eastus)      │
       │                                                        │
       │   ┌────────────────────────────────────────────┐       │
       │   │  vnet-miproposito-aks   10.10.0.0/16       │       │
       │   │                                            │       │
       │   │  ├─ waf-subnet      10.10.0.0/24           │       │
       │   │  │   (App Gateway WAF v2 — OFF por default)│       │
       │   │  │                                         │       │
       │   │  ├─ lb-subnet       10.10.1.0/24           │       │
       │   │  │   (NGINX Ingress LoadBalancer público)  │       │
       │   │  │                                         │       │
       │   │  ├─ aks-subnet      10.10.16.0/20          │       │
       │   │  │   (pods de los 7 microservicios)        │       │
       │   │  │                                         │       │
       │   │  └─ sql-pe-subnet   10.10.32.0/24          │       │
       │   │      (Private Endpoint a Azure SQL)        │       │
       │   └────────────────────────────────────────────┘       │
       │                                                        │
       │   AKS (Free tier, 3 nodes Standard_D2s_v3)             │
       │   ACR Standard                                         │
       │   Key Vault (RBAC, CSI driver)                         │
       │   Redis Standard C1 (TLS 1.2)                          │
       │   Storage Account (LRS, blob privado, container exports)│
       │   SQL Server (publicNetworkAccess: Disabled)           │
       │   ├─ db_users                                          │
       │   ├─ db_exams                                          │
       │   ├─ db_keys                                           │
       │   └─ db_satisfaction       (todas GP_S_Gen5_2 serverless)│
       │                                                        │
       └────────────────────────────────────────────────────────┘
                                ▲
                                │ git push → pipeline
                                │
              ┌─────────────────┴─────────────────┐
              │  Azure DevOps                     │
              │  ├─ Repos    (código)             │
              │  ├─ Pipelines (build + deploy)    │
              │  ├─ Boards   (tickets)            │
              │  └─ Library  (Variable Groups)    │
              └───────────────────────────────────┘
```

### Decisiones de costo/seguridad

| Decisión | Por qué |
|---|---|
| **App Gateway OFF por default** (`-EnableAppGateway` switch) | ~$300/mes. NGINX Ingress + Public IP cubre staging. El cliente puede activar WAF cuando esté listo a pagarlo. |
| **API Management ❌ NO se usa** | ~$300/mes. El gateway propio en Go (`gateway/`, grpc-gateway) hace el trabajo: REST↔gRPC, JWT validation, CORS, rate limit. APIM es overkill mientras el frontend siga siendo único cliente. |
| **AKS Free tier** | 99.5% SLA, $0/mes. Suficiente para staging y producción inicial. |
| **SQL serverless GP_S_Gen5_2** | Auto-pause después de 1h sin uso → barato en dev. Si la latencia molesta en prod, escalar a Provisioned. |
| **SQL `publicNetworkAccess: Disabled`** | Solo accesible desde AKS vía Private Endpoint. Nada de firewall reglas IP. |
| **Key Vault con `publicNetworkAccess: Enabled`** | Provisional — simplifica el bootstrap (`az keyvault secret set` desde la PC del admin). En endurecimiento post-MVP, agregar PE y deshabilitar. |
| **2 Service Connections, no 3** | Una sola conexión ARM (`azure-subscription`) hace `az aks get-credentials` + `helm upgrade`. No hace falta una Kubernetes service connection separada. |

---

## Microservicios (qué se deploya)

Todos viven en namespace `miproposito` del AKS. Comunicación interna: **gRPC**.
Frontend Angular consume **REST** vía gateway (grpc-gateway traduce).

| Servicio | Lang | DB | Notas |
|---|---|---|---|
| `users-service` | Go | `db_users` | JWT signer/verifier, permisos `<svc>.<table>.<action>`, audit log, asignaciones SCD2 (`assignment` table) |
| `exams-service` | Go | `db_exams` | Sirve los 3 módulos via `exam_type.code` (vocacional/simulacro/habitos). Versionado por `parent_exam_id` |
| `keys-service` | Go | `db_keys` | Códigos de acceso a tests (modos LAN/colegio, aforos) |
| `satisfaction-service` | Go | `db_satisfaction` | Encuestas NPS/escala/abierta post-test |
| `analytics-service` | Go | (sin DB) | Agregaciones via gRPC clients a otros servicios |
| `hubspot-service` | Node.js (server + worker) | (sin DB, usa Redis) | CRM v3 + Custom Objects + Webhooks. RequestsEngine con rate limit distribuido vía Redis (`redis_rate.Limiter`, Lua atómico). Tokens HubSpot rotables CSV en Key Vault para multiplicar RPS |
| `gateway` | Go (grpc-gateway) | (sin DB) | REST↔gRPC, JWT validation, CORS, rate limit |

**Auth**: JWT HS256 access (15min) + refresh (7d con `jti` revocable en `db_users.refresh_token`). Header `Authorization: Bearer <token>`. OTP estudiantes vía workflow webhook HubSpot (no SMS).

**Audit log**: tabla `audit_log` por servicio. Interceptor gRPC captura todas las mutaciones con `actor_user_id`, IP, correlation_id, before/after JSON.

**Permisos cross-service**: `permmw` interceptor (en `users-service` y reusado vía `pkg/permmw`) llama a `users-service.HasPermission` con cache Redis 30s.

---

## Estructura de archivos relevantes (DevOps)

```
mi_proposito/
├── azure-pipelines.yml           ← pipeline 3-stage (test → images → deploy)
├── READMEDevops.md               ← ESTE archivo
├── deploy/
│   ├── INSTALL.md                ← guía maestra 5 fases (cliente)
│   ├── install.ps1               ← installer 1-click PowerShell
│   ├── smoke-test.ps1            ← health checks post-deploy
│   ├── smoke-test.sh             ← idem en bash (lo corre el pipeline)
│   ├── azure-devops/
│   │   └── README.md             ← setup Service Connections + Variable Group + Environment
│   ├── bicep/
│   │   ├── main.bicep            ← orquestador (params: namePrefix, location, sqlAdminLogin/Password, adminObjectId, enableAppGateway)
│   │   └── modules/
│   │       ├── network.bicep     ← VNet + 4 subnets + 4 NSGs
│   │       ├── aks.bicep         ← AKS Free + Workload Identity + KV CSI addon
│   │       ├── sql.bicep         ← SQL Server private + 4 DBs + Private Endpoint + Private DNS Zone
│   │       ├── keyvault.bicep    ← KV con RBAC (admin → Administrator, kubelet → Secrets User)
│   │       ├── acr.bicep         ← ACR Standard
│   │       ├── storage.bicep     ← Storage LRS, blob privado, container "exports"
│   │       ├── redis.bicep       ← Redis Standard C1, TLS 1.2
│   │       └── app-gateway.bicep ← (opcional, OFF default)
│   └── helm/
│       ├── miproposito/          ← chart umbrella
│       │   ├── Chart.yaml
│       │   ├── values.yaml
│       │   ├── values.production.yaml
│       │   └── templates/        ← cross-cutting (NetworkPolicies, ServiceMonitor)
│       └── charts/
│           ├── users-service/
│           ├── exams-service/
│           ├── keys-service/
│           ├── satisfaction-service/
│           ├── hubspot-service/
│           ├── analytics-service/
│           └── gateway/
│               (cada uno con: deployment.yaml, service.yaml, configmap.yaml,
│                secret-provider-class.yaml, hpa.yaml, job-migrate.yaml, job-seed.yaml)
├── proto/                        ← .proto compartidos (common/error, common/search, common/audit)
├── pkg/                          ← libs Go compartidas (apperr, search, jwtmw, auditmw, mssql, migrator, permmw, requestsengine)
├── users_service/
├── exams_service/
├── keys_service/
├── satisfaction_service/
├── hubspot_service/
├── analytics_service/
└── gateway/
```

---

## Flujo de instalación (5 fases)

### Fase 1 — Prerrequisitos en la PC del admin

- Azure CLI (`az version`)
- kubectl (`az aks install-cli`)
- Helm 3 (`winget install Helm.Helm`)
- Git
- `az login` con cuenta que tenga rol **Owner** o **Contributor + User Access Administrator** sobre la suscripción

### Fase 2 — Provisionar Azure (1 comando, ~25 min)

```powershell
.\deploy\install.ps1 -ResourceGroup rg-miproposito -Location eastus
```

Lo que hace:
1. Verifica prereq.
2. Verifica `az login`.
3. Crea/reusa el RG.
4. Genera SQL admin password de 24 chars y la persiste a `.deploy-state/sql-admin-password.txt` (con `.gitignore` `*` autocreado).
5. `az deployment group create` del Bicep modular.
6. Carga secretos iniciales al Key Vault:
   - `users-service-sql-password` (= sqlPwd)
   - `users-service-redis-password` (= redis primary key)
   - `users-service-jwt-secret` (64 chars random)
   - `exams-service-sql-password`, `keys-service-sql-password`, `satisfaction-service-sql-password`
   - `analytics-service-redis-password`, `gateway-redis-password`
   - `hubspot-service-api-tokens` (placeholder)
   - `hubspot-service-otp-webhook-token` (placeholder)
   - `hubspot-service-redis-password`
7. `az aks get-credentials` (descarga kubeconfig).
8. Imprime variables que copiar al Variable Group de Azure DevOps.

**Idempotente** — re-correrlo no rompe nada (Bicep declarativo, `az group exists` chequeado).

### Fase 3 — Configurar Azure DevOps (~15 min)

Ver `deploy/azure-devops/README.md`. Resumen:

1. Crear org + project.
2. `git push` el repo a Azure Repos.
3. Crear 2 Service Connections:
   - **`azure-subscription`** (ARM, Service Principal automatic, scope = subscription)
   - **`acr-connection`** (Docker Registry → Azure Container Registry → SP automatic)
4. Crear Variable Group **`miproposito-cicd`** con todas las variables que imprimió `install.ps1` (acrName, aksClusterName, aksResourceGroup, keyVaultName, sqlServerFqdn, redisHost, redisSslPort, tenantId, secretsProviderClientId, serviceConnectionArm=`azure-subscription`, serviceConnectionAcr=`acr-connection`).
5. Crear Environment **`miproposito-prod`** con manual approval gate.
6. Habilitar pipeline desde el `azure-pipelines.yml` raíz.

### Fase 4 — Primer deploy (~15 min)

```powershell
git push origin main
```

Stages que corren:
1. **Test** (~3 min): `go build + go vet + go test -race -cover ./...` por cada servicio Go.
2. **Images** (~8 min): docker build multi-target (server + migrate + worker) y push a ACR. Imágenes etiquetadas con `$(Build.BuildId)`.
3. **Deploy** (manual approval): `az aks get-credentials` + `helm dependency build` + `helm upgrade --install miproposito ./deploy/helm/miproposito -f values.production.yaml --set global.imageTag=$(imageTag)`.

Helm corre los Jobs `*-migrate` (T-SQL) en helm hook `pre-install,pre-upgrade`, y los `*-seed` en `post-install`. Después levanta los Deployments.

Después del primer deploy, crear el superadmin:
```powershell
kubectl exec -n miproposito deploy/users-service -- /app/migrate seed-superadmin `
  --email "admin@ucsp.edu.pe" `
  --password "TuPasswordSegura123!"
```

### Fase 5 — Reemplazar tokens HubSpot

```powershell
az keyvault secret set --vault-name $kv --name hubspot-service-api-tokens `
  --value "pat-na1-xxxx,pat-na1-yyyy,pat-na1-zzzz"
az keyvault secret set --vault-name $kv --name hubspot-service-otp-webhook-token `
  --value "<token>"
kubectl rollout restart deploy/hubspot-service -n miproposito
kubectl rollout restart deploy/hubspot-service-worker -n miproposito
```

---

## Pipeline (`azure-pipelines.yml`) — qué tener en cuenta

Variables consumidas del Variable Group `miproposito-cicd`:
- `acrName`, `aksResourceGroup`, `aksClusterName`
- `serviceConnectionArm` (= `azure-subscription`) — ARM connection para `az` y helm
- `serviceConnectionAcr` (= `acr-connection`) — Docker Registry para `Docker@2 login`

Triggers:
- `main` push con path filters (`<service>/**`, `deploy/**`, `azure-pipelines.yml`).
- PRs a `main` (corre Test + Images, NO Deploy).

Imágenes construidas por servicio:
- `users-service` (server, migrate)
- `exams-service` (server, migrate)
- `keys-service` (server, migrate)
- `satisfaction-service` (server, migrate)
- `hubspot-service` (server, worker)
- `analytics-service` (server)
- `gateway` (server)

Total: **11 imágenes** por build. Cada Dockerfile tiene multi-target (`--target server`, `--target migrate`, `--target worker`).

---

## Bug fixes / decisiones recientes (2026-05-03)

1. **Bug en `azure-pipelines.yml`**: usaba `serviceConnectionAks` (Kubernetes type) como `azureSubscription` parameter de `AzureCLI@2`, que requiere ARM. Cambiado a `serviceConnectionArm` (= `azure-subscription`). Eliminada la necesidad de Kubernetes service connection: `az aks get-credentials` desde ARM connection es suficiente.
2. **Reducción de Service Connections de 3 a 2**: simplifica el setup para el cliente no-experto.
3. **Storage container `exports`**: pre-creado por Bicep (lo usa `analytics-service` para los Excel exports con `xuri/excelize`).
4. **App Gateway OFF por default**: ahorra $300/mes; el cliente puede activarlo con `-EnableAppGateway` cuando quiera.

---

## Cosas que **faltan** o son seguimiento

- [ ] Commit + push del trabajo reciente al repo Azure DevOps (el usuario aún no lo subió).
- [ ] Ejecutar `install.ps1` en la suscripción Azure pago del cliente (cuenta nueva con $200 de crédito).
- [ ] Configurar Azure DevOps siguiendo `deploy/azure-devops/README.md`.
- [ ] Conseguir los tokens reales HubSpot (el cliente tiene que crear las Private Apps en su portal HS).
- [ ] DNS: apuntar `api.miproposito.ucsp.edu.pe` al Public IP del NGINX Ingress (lo da `kubectl get svc -n ingress-nginx`).
- [ ] cert-manager + ClusterIssuer Let's Encrypt para TLS automático (el chart umbrella ya tiene los hooks, falta `helm install cert-manager` antes del primer deploy).
- [ ] Endurecer Key Vault: agregar Private Endpoint y `publicNetworkAccess: Disabled` post-MVP.
- [ ] Branch policy en `main` (PR + 1 reviewer + tests verdes).

---

## Comandos de referencia rápida

```powershell
# Ver el deployment Bicep más reciente
az deployment group list -g rg-miproposito --query "[0].{name:name, state:properties.provisioningState}" -o table

# Ver outputs de un deployment puntual
az deployment group show -g rg-miproposito --name <deploymentName> --query properties.outputs

# Ver pods
kubectl get pods -n miproposito -o wide

# Logs de un servicio
kubectl logs -n miproposito deploy/users-service -f

# Ver secretos en Key Vault
az keyvault secret list --vault-name <kvName> --query "[].name" -o tsv

# Rollback de un helm release
helm rollback miproposito 1 -n miproposito

# Conectarse a una DB Azure SQL (desde un pod, porque public access está OFF)
kubectl run -it --rm sqlcmd --image=mcr.microsoft.com/mssql-tools -n miproposito -- bash
# adentro: sqlcmd -S <sqlServerFqdn> -U miprop_admin -P '<password>' -d db_users
```

---

## Convenciones del repo (importante para Claude)

- **Idioma**: comentarios y docs en **español** (cliente peruano). Código y nombres de variables en inglés.
- **Sin atribuciones a Claude**: el cliente NO debe ver "Claude" en commits, archivos ni documentación. Memoria del usuario: `feedback_no_claude_in_commits.md`.
- **Sin emojis** salvo que el usuario los pida.
- **Sin comentarios obvios** en código — solo cuando el WHY es no-trivial.
- **Hexagonal + CQRS + SOLID** estricto en todos los servicios Go. `core/` no importa nada de `adapters/`.
- **Tests con testcontainers-go** (SQL Server image `mcr.microsoft.com/mssql/server:2022-latest`) en `internal/adapters/outbound/mssql/*_test.go`.
- **Migrations**: T-SQL embebido vía `go:embed`, ejecutado por el binario `cmd/migrate/` con tabla `__schema_migrations(version, applied_at, checksum)`.

---

## Para el próximo Claude que lea esto

Si el usuario te pide:
- **"deploy"** o **"instalar"** → seguir las 5 fases de `deploy/INSTALL.md`. NO ejecutar comandos `az` que cuesten dinero sin confirmación.
- **"agregar un microservicio nuevo"** → copiar la estructura de `users_service/` (hexagonal), agregar al `azure-pipelines.yml` (`SERVICES` array y trigger paths), crear chart en `deploy/helm/charts/<nombre>/`, agregar dependency al umbrella en `deploy/helm/miproposito/Chart.yaml`.
- **"cambiar región / nombres"** → editar parámetros de `install.ps1`, no hardcodear en Bicep.
- **"hacer un cambio en producción"** → SIEMPRE pasar por PR + pipeline, NUNCA `kubectl apply` o `helm upgrade` manualmente desde la laptop.
- **"el pod está en CrashLoopBackOff"** → primero `kubectl logs --previous`, después `kubectl describe pod`, después chequear que `SecretProviderClass` haya montado los secretos.

Si necesitás el plan original completo (las 9 fases de implementación de los microservicios), está en `C:\Users\angel\.claude\plans\hola-leete-todo-el-lexical-meerkat.md`.

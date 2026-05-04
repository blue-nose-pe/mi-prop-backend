# Mi Propósito 2.0 — Guía de instalación (5 fases)

Esta guía deja todo Mi Propósito 2.0 corriendo en Azure desde cero.
Está pensada para alguien sin experiencia previa con Kubernetes —
cada paso está explicado y todos los comandos son copy-paste.

**Tiempo total: 60–90 minutos** (la primera vez).

```
┌─────────────────────────────────────────────────────────────────────┐
│  Fase 1 — Prerrequisitos en tu PC                       (10 min)    │
│  Fase 2 — Provisionar infraestructura Azure             (25 min)    │
│  Fase 3 — Conectar Azure DevOps                         (15 min)    │
│  Fase 4 — Primer deploy automático vía pipeline         (15 min)    │
│  Fase 5 — Configurar Key Vault con tokens reales        ( 5 min)    │
└─────────────────────────────────────────────────────────────────────┘
```

Lo que vamos a desplegar (lo dibuja el cliente en su diagrama):

```
                 ┌──────────────────────────────────────────────┐
                 │   Resource Group: rg-miproposito             │
                 │                                              │
   Internet ───▶ │   ┌────────────────────────────────────┐     │
                 │   │  vnet-miproposito-aks  10.10/16     │     │
                 │   │  ├─ waf-subnet      (App Gw — off)  │     │
                 │   │  ├─ lb-subnet       (NGINX Ingress) │     │
                 │   │  ├─ aks-subnet      (pods Go/Node)  │     │
                 │   │  └─ sql-pe-subnet   (Private Endpt) │     │
                 │   └────────────────────────────────────┘     │
                 │                                              │
                 │   AKS · ACR · Key Vault · Redis · Storage    │
                 │   Azure SQL Server (private only) + 4 BDs    │
                 │                                              │
                 └──────────────────────────────────────────────┘
                                    ▲
                                    │ git push (CI/CD)
                                    │
                   ┌────────────────┴───────────────┐
                   │  Azure DevOps                  │
                   │  ├─ Repos (código)             │
                   │  ├─ Pipelines (build + deploy) │
                   │  └─ Boards (tickets)           │
                   └────────────────────────────────┘
```

---

## Fase 1 — Prerrequisitos en tu PC

### 1.1 Instalar herramientas

| Herramienta | Verificar          | Instalar                                 |
|-------------|--------------------|------------------------------------------|
| Azure CLI   | `az version`       | https://aka.ms/installazurecliwindows    |
| kubectl     | `kubectl version --client` | `az aks install-cli`             |
| Helm 3      | `helm version`     | `winget install Helm.Helm`               |
| Git         | `git --version`    | https://git-scm.com/download/win         |

### 1.2 Verificar tu acceso a Azure

```powershell
az login
az account show --query "{Subscription:name, Tenant:tenantName}" -o table
```

> Si tenés varias suscripciones, fijala como default:
> `az account set --subscription "Mi-Subscription"`

### 1.3 Permisos requeridos

Sobre la suscripción Azure necesitás:
- **Owner** (recomendado para la primera vez), o
- **Contributor** + **User Access Administrator** (mínimo).

Si no tenés esos roles, pedile al admin de tu tenant que te los asigne
o que ejecute la Fase 2 él mismo.

---

## Fase 2 — Provisionar la infraestructura Azure

Un solo comando aprovisiona todo: VNet, AKS, SQL Server + 4 BDs,
Key Vault, ACR, Redis, Storage. Es **idempotente** — re-correrlo
no rompe nada.

### 2.1 Clonar el repo (si todavía no lo hiciste)

```powershell
git clone <URL_REPO> miproposito
cd miproposito
```

### 2.2 Ejecutar el instalador

```powershell
.\deploy\install.ps1 -ResourceGroup rg-miproposito -Location eastus
```

**Tarda 15–25 minutos.** Mientras corre, lo que hace:

1. ✅ Verifica que `az`, `kubectl`, `helm` estén instalados.
2. ✅ Verifica que estés logueado en Azure.
3. ✅ Crea el Resource Group.
4. ✅ Genera y guarda localmente la password del SQL admin
   (`.deploy-state/sql-admin-password.txt` — **hacé backup en tu
   password manager**).
5. ✅ Despliega el Bicep modular (~10 min).
6. ✅ Carga secretos iniciales al Key Vault (SQL, Redis, JWT, placeholders HubSpot).
7. ✅ Descarga el kubeconfig y conecta `kubectl` al AKS.
8. ✅ Imprime la lista de variables que vas a pegar en Azure DevOps.

### 2.3 Verificar el resultado

```powershell
kubectl get nodes
# Esperás: 3 nodos en estado "Ready"

az sql db list -g rg-miproposito --server <sql-server-name> --query "[].name" -o tsv
# Esperás: db_users, db_exams, db_keys, db_satisfaction
```

> **Guardá la salida de `install.ps1`** — tiene los nombres exactos
> que vas a copiar en la Fase 3.

### 2.4 ¿Querés ahorrar dinero?

El default ya es **el más barato razonable** (~$120/mes con $200 de
crédito). Si querés bajar más:
- AKS: cambiá `vmSize: Standard_D2s_v3` → `Standard_B2s` en
  [aks.bicep](bicep/modules/aks.bicep). Ahorrás ~$50/mes pero pierdes
  performance.
- SQL: las 4 DBs son `GP_S_Gen5_2` (serverless con auto-pause).
  En desarrollo, podés bajar `maxSizeBytes` y `maxCapacity` en
  [sql.bicep](bicep/modules/sql.bicep).

App Gateway WAF está **desactivado por default** (te ahorra ~$300/mes).
Si lo querés activar después: `.\deploy\install.ps1 -EnableAppGateway`.

---

## Fase 3 — Conectar Azure DevOps

> Esta fase está completamente documentada en
> [azure-devops/README.md](azure-devops/README.md). Resumen acá:

1. Crear organización + proyecto Azure DevOps.
2. Subir el repo a Azure Repos (`git remote add origin ... ; git push`).
3. Crear 2 Service Connections:
   - **`azure-subscription`** (ARM con Service Principal)
   - **`acr-connection`** (Docker Registry → ACR)
4. Crear el Variable Group **`miproposito-cicd`** y pegarle los valores
   que imprimió `install.ps1`.
5. Crear el Environment **`miproposito-prod`** con manual approval.
6. Habilitar el pipeline `azure-pipelines.yml`.

**Seguí los pasos exactos en [azure-devops/README.md](azure-devops/README.md).**

---

## Fase 4 — Primer deploy automático

Una vez configurado Azure DevOps, hacé tu primer push:

```powershell
git add .
git commit -m "feat: initial setup"
git push origin main
```

Andá a **Azure DevOps → Pipelines → mi-proposito**. Vas a ver:

```
Stage 1: Test     ▶  ~3 min  (go test ./... en los 7 servicios)
Stage 2: Images   ▶  ~8 min  (docker build + push de 11 imágenes)
Stage 3: Deploy   ▶  pendiente — pide aprobación manual
```

Aprobá el deploy. El stage `Deploy`:
- Hace `helm dependency build` en el chart umbrella.
- Hace `helm upgrade --install miproposito ./deploy/helm/miproposito`.
- Corre los Jobs `*-migrate` (crean tablas en cada DB).
- Corre los Jobs `*-seed` (cargan datos iniciales: roles, permisos, exam_types).
- Levanta los Deployments (gateway + 6 microservicios + worker hubspot).
- Corre el smoke test contra `/health` de cada servicio.

### Verificar que todo está vivo

```powershell
kubectl get pods -n miproposito
# Esperás todos en estado "Running" (puede tardar 2-3 min en estabilizar)

kubectl get svc -n miproposito
# Buscá la EXTERNAL-IP del service "ingress-nginx-controller"
```

Esa IP externa es la que apunta tu DNS público al backend.

### Crear el primer usuario admin

```powershell
kubectl exec -n miproposito deploy/users-service -- /app/migrate seed-superadmin `
  --email "admin@ucsp.edu.pe" `
  --password "TuPasswordSegura123!"
```

Ahora podés hacer login:

```powershell
$ip = kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
curl -X POST "http://$ip/api/auth/login" -H "Content-Type: application/json" `
  -d '{"email":"admin@ucsp.edu.pe","password":"TuPasswordSegura123!"}'
```

Esperás un JSON con `access_token` + `refresh_token`. ✅

---

## Fase 5 — Reemplazar tokens HubSpot

`install.ps1` cargó **placeholders** para los tokens de HubSpot. Hay
que reemplazarlos con los reales antes de que `hubspot-service`
funcione.

### 5.1 Conseguir el Private App Token de HubSpot

1. En HubSpot: **Settings → Integrations → Private Apps → Create private app**.
2. Scopes mínimos: `crm.objects.contacts.read/write`,
   `crm.objects.custom.read/write`, `automation`, `settings.users.write`.
3. Copia el token (formato `pat-na1-xxxx`).

> Tip: HubSpot permite **hasta 10 private apps por portal**. Creá 4-6
> apps con permisos idénticos para multiplicar tu RPS efectivo
> (`hubspot-service` los rota automáticamente vía Redis con rate
> limit distribuido).

### 5.2 Conseguir el OTP webhook trigger

1. En HubSpot: **Automation → Workflows → New workflow**.
2. Trigger: contacto entra cuando `enviar_otp = true`.
3. Action: **Send email** con la propiedad `{{ contact.otp_estudiante }}`.
4. **Settings → Webhooks → Trigger workflow via webhook**: copiá
   el URL `https://api-na1.hubapi.com/automation/v4/webhook-triggers/{ID}/{TOKEN}`.

### 5.3 Actualizar el Key Vault

```powershell
$kv = "<keyVaultName de la Fase 2>"

# Tokens HubSpot (CSV con todos para multiplicar RPS):
az keyvault secret set --vault-name $kv `
  --name hubspot-service-api-tokens `
  --value "pat-na1-xxxxx,pat-na1-yyyyy,pat-na1-zzzzz"

# OTP webhook token:
az keyvault secret set --vault-name $kv `
  --name hubspot-service-otp-webhook-token `
  --value "<TOKEN del paso 5.2>"
```

### 5.4 Reiniciar `hubspot-service` para que tome los nuevos secretos

```powershell
kubectl rollout restart deploy/hubspot-service -n miproposito
kubectl rollout restart deploy/hubspot-service-worker -n miproposito
```

Esperás que los pods vuelvan a `Running`. Listo. ✅

---

## ¿Y ahora qué?

- **Cada `git push` a `main`** → pipeline + deploy automático.
- **Cada PR** → pipeline corre tests sin deployar (gating).
- **Branch policies**: protegé `main` (PR + 1 reviewer + tests verdes).
- **Smoke tests**: corren después de cada deploy. Si fallan, el pipeline
  queda en rojo y podés rollbackear con `helm rollback miproposito 1 -n miproposito`.
- **Logs**: `kubectl logs -n miproposito deploy/<servicio> -f`.
- **Métricas**: `kubectl top pods -n miproposito`.
- **APIs documentadas**: ver [api-docs/README.md](api-docs/README.md).

---

## Troubleshooting frecuente

### `install.ps1` falla en el step de Bicep
```
The deployment 'miprop-...' failed with error: ...
```
Revisá el detalle:
```powershell
az deployment group show -g rg-miproposito --name miprop-... `
  --query "properties.error" -o json
```
Las causas más comunes:
- **Quota insuficiente**: pedí más cores en tu región (Subscription → Usage + quotas).
- **Nombre de recurso ya tomado**: el ACR y el Key Vault son globalmente únicos.
  Cambiá `-NamePrefix` a algo más raro y reintentá.

### `kubectl` no se conecta al cluster
```powershell
az aks get-credentials -g rg-miproposito -n aks-miproposito --overwrite-existing
```

### Pods en `CrashLoopBackOff`
Lo más común: el SecretProviderClass no logra montar secretos del Key Vault.

```powershell
kubectl logs -n miproposito <pod-name> --previous
kubectl describe pod -n miproposito <pod-name> | Select-String "Secret"
```

Verificá que la identidad del addon `azureKeyvaultSecretsProvider` tiene
rol **Key Vault Secrets User** sobre el KV (lo asigna el Bicep automáticamente
en [keyvault.bicep](bicep/modules/keyvault.bicep)).

### El primer login devuelve 401
Olvidaste correr `seed-superadmin` (Fase 4). El step de seed inicial
crea roles y permisos pero NO crea un usuario admin con password —
hay que correr el comando manual una vez.

---

## Estructura de archivos relevantes

```
deploy/
├── install.ps1                         ← Fase 2 (1-click installer)
├── INSTALL.md                          ← Este archivo
├── smoke-test.ps1                      ← post-deploy health checks
├── azure-devops/
│   └── README.md                       ← Fase 3 (Service Connections + Variable Group)
├── bicep/
│   ├── main.bicep                      ← orquesta los 7 módulos
│   └── modules/
│       ├── network.bicep               ← VNet + 4 subnets + NSGs
│       ├── aks.bicep                   ← AKS + Workload Identity
│       ├── sql.bicep                   ← SQL Server + 4 DBs + Private Endpoint
│       ├── keyvault.bicep              ← Key Vault con RBAC
│       ├── acr.bicep
│       ├── storage.bicep
│       ├── redis.bicep
│       └── app-gateway.bicep           ← (opcional, off por default)
└── helm/
    ├── miproposito/                    ← chart umbrella (deploy completo)
    └── charts/
        ├── users-service/
        ├── exams-service/
        ├── keys-service/
        ├── satisfaction-service/
        ├── hubspot-service/
        ├── analytics-service/
        └── gateway/
```

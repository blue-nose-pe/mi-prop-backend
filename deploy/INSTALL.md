# Mi Propósito 2.0 — Guía de instalación

Esta guía describe cómo desplegar Mi Propósito 2.0 en Azure desde cero.
Está pensada para alguien con experiencia básica en línea de comandos
de Azure; **no requiere experiencia previa con Kubernetes**: cada paso
está explicado y todos los comandos son copy-paste.

Tiempo estimado primera vez: **60–90 minutos**.

---

## 1. Prerrequisitos en tu máquina (Windows / PowerShell)

| Herramienta | Versión mínima | Verificar      | Instalar                                            |
|-------------|----------------|----------------|-----------------------------------------------------|
| Azure CLI   | 2.55           | `az version`   | https://aka.ms/installazurecliwindows                |
| kubectl     | 1.28           | `kubectl version --client` | `az aks install-cli`                     |
| Helm        | 3.13           | `helm version` | https://helm.sh/docs/intro/install/                  |
| Docker      | 24             | `docker --version` | https://docs.docker.com/desktop/                |

Necesitás también:
- Una **suscripción Azure** activa con permisos de **Owner** o de **Contributor + User Access Administrator** sobre un Resource Group.
- Tu **Object ID** de Azure AD (`az ad signed-in-user show --query id -o tsv`).

---

## 2. Clonar el repositorio

```powershell
git clone <REPO_URL> miproposito
cd miproposito
```

---

## 3. Aprovisionar la infraestructura Azure (una sola vez por entorno)

Crea: SQL Server + 4 BDs (`db_users`, `db_exams`, `db_keys`, `db_satisfaction`)
+ Redis + Key Vault + ACR.

```powershell
az login

# Resource group
az group create -n rg-miproposito -l eastus

# Object ID propio (para asignarse permisos en el Key Vault)
$myObjectId = az ad signed-in-user show --query id -o tsv

# Password fuerte para el SQL admin (guardarla en un password manager)
$sqlPwd = -join ((33..126) | Get-Random -Count 24 | ForEach-Object {[char]$_})
Write-Host "GUARDA ESTA PASSWORD EN UN LUGAR SEGURO: $sqlPwd"

# Aprovisionar
az deployment group create `
  -g rg-miproposito `
  -f deploy/bicep/main.bicep `
  -p sqlAdminLogin=miprop_admin `
  -p sqlAdminPassword=$sqlPwd `
  -p keyVaultAdminObjectId=$myObjectId
```

Anotá los outputs (los necesita el `values.production.yaml`):

```powershell
az deployment group show -g rg-miproposito -n main --query properties.outputs
```

---

## 4. Crear el cluster AKS

```powershell
az aks create `
  -g rg-miproposito `
  -n aks-miproposito `
  --node-count 3 `
  --node-vm-size Standard_D2s_v3 `
  --enable-managed-identity `
  --enable-addons azure-keyvault-secrets-provider `
  --enable-workload-identity `
  --enable-oidc-issuer `
  --attach-acr miprepositoacr

# Obtener kubeconfig
az aks get-credentials -g rg-miproposito -n aks-miproposito

# Verificar
kubectl get nodes
```

---

## 5. Cargar secretos al Key Vault

Por cada microservicio que necesite secretos, cargar:

```powershell
$kv = "miproposito-kv"

# users-service
az keyvault secret set --vault-name $kv --name users-service-sql-password    --value $sqlPwd
az keyvault secret set --vault-name $kv --name users-service-redis-password  --value (az redis list-keys -g rg-miproposito -n miproposito-redis --query primaryKey -o tsv)
$jwtSecret = -join ((33..126) | Get-Random -Count 64 | ForEach-Object {[char]$_})
az keyvault secret set --vault-name $kv --name users-service-jwt-secret      --value $jwtSecret

# exams-service
az keyvault secret set --vault-name $kv --name exams-service-sql-password --value $sqlPwd

# keys-service
az keyvault secret set --vault-name $kv --name keys-service-sql-password --value $sqlPwd

# satisfaction-service
az keyvault secret set --vault-name $kv --name satisfaction-service-sql-password --value $sqlPwd

# hubspot-service
# IMPORTANTE: el secret es PLURAL (api-tokens). Acepta una lista CSV
# de API keys de HubSpot — el engine las rota entre TODAS las réplicas
# del servicio para no superar el rate limit (10 rps por key).
# Si solo tenés una key, igual mandala como CSV de un elemento.
az keyvault secret set --vault-name $kv --name hubspot-service-api-tokens       --value "<HUBSPOT_TOKEN_1>,<HUBSPOT_TOKEN_2>,<HUBSPOT_TOKEN_3>"
az keyvault secret set --vault-name $kv --name hubspot-service-otp-webhook-token --value "<HUBSPOT_OTP_WEBHOOK_TOKEN>"
az keyvault secret set --vault-name $kv --name hubspot-service-redis-password   --value (az redis list-keys -g rg-miproposito -n miproposito-redis --query primaryKey -o tsv)

# analytics-service
az keyvault secret set --vault-name $kv --name analytics-service-redis-password --value (az redis list-keys -g rg-miproposito -n miproposito-redis --query primaryKey -o tsv)

# gateway
az keyvault secret set --vault-name $kv --name gateway-redis-password --value (az redis list-keys -g rg-miproposito -n miproposito-redis --query primaryKey -o tsv)
```

> El **JWT secret** se carga **solo una vez** (en `users-service-jwt-secret`).
> Los demás servicios lo leen del mismo objeto de Key Vault — comparten
> emisor/verificador. Ver `secret-provider-class.yaml` de cada chart.

---

## 6. Instalar dependencias del cluster

```powershell
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo add jetstack       https://charts.jetstack.io
helm repo update

# Ingress NGINX
helm install ingress-nginx ingress-nginx/ingress-nginx -n ingress-nginx --create-namespace

# cert-manager (TLS automático Let's Encrypt)
helm install cert-manager jetstack/cert-manager -n cert-manager --create-namespace --set installCRDs=true
```

Crear el `ClusterIssuer`:

```powershell
kubectl apply -f - <<'EOF'
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: pablo.perez@bluenose.pe
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
      - http01:
          ingress:
            class: nginx
EOF
```

---

## 7. Construir y publicar las imágenes Docker

Cada microservicio tiene un `Dockerfile` multi-target. Construir y empujar
a ACR:

```powershell
az acr login --name miprepositoacr

$REG = "miprepositoacr.azurecr.io"
$TAG = "0.1.0"

# server + migrate (Go services con DB)
foreach ($svc in @("users_service","exams_service","keys_service","satisfaction_service")) {
  $base = $svc -replace "_","-"
  docker build --target server  -t "$REG/${base}:$TAG"          $svc
  docker build --target migrate -t "$REG/${base}-migrate:$TAG" $svc
  docker push "$REG/${base}:$TAG"
  docker push "$REG/${base}-migrate:$TAG"
}

# hubspot: server + worker
docker build --target server -t "$REG/hubspot-service:$TAG"        hubspot_service
docker build --target worker -t "$REG/hubspot-service-worker:$TAG" hubspot_service
docker push "$REG/hubspot-service:$TAG"
docker push "$REG/hubspot-service-worker:$TAG"

# analytics + gateway: solo server
docker build --target server -t "$REG/analytics-service:$TAG" analytics_service
docker push "$REG/analytics-service:$TAG"

docker build --target server -t "$REG/gateway:$TAG" gateway
docker push "$REG/gateway:$TAG"
```

> En **Azure DevOps** este paso lo hace el pipeline automáticamente
> (ver `azure-pipelines.yml`).

---

## 8. Configurar `values.production.yaml`

Editá `deploy/helm/miproposito/values.production.yaml` y reemplazar:
- `<TENANT-ID>` con el tenant ID de Azure AD
- `<UAMI-CLIENT-ID>` con el clientId de la User-Assigned Managed Identity del addon `azure-keyvault-secrets-provider`

```powershell
$tenantId = az account show --query tenantId -o tsv
$uami = az aks show -g rg-miproposito -n aks-miproposito `
  --query addonProfiles.azureKeyvaultSecretsProvider.identity.clientId -o tsv

(Get-Content deploy/helm/miproposito/values.production.yaml) `
  -replace '<TENANT-ID>', $tenantId `
  -replace '<UAMI-CLIENT-ID>', $uami `
  | Set-Content deploy/helm/miproposito/values.production.yaml
```

---

## 9. Instalar Mi Propósito en AKS

```powershell
cd deploy/helm/miproposito
helm dependency build

helm install miproposito . `
  -f values.production.yaml `
  --set global.imageTag=0.1.0 `
  -n miproposito --create-namespace
```

**Lo que pasa internamente:**

1. **`pre-install` hooks** → Jobs `*-migrate` aplican las migraciones T-SQL
   contra Azure SQL (idempotentes; las ya aplicadas se saltan).
2. **Jobs completan** → Helm crea Deployments / Services / SecretProviderClass / HPA.
3. **Pods arrancan** → leen secretos de Key Vault, conectan a Azure SQL y Redis.
4. **gateway expone** `api.miproposito.ucsp.edu.pe` vía Ingress NGINX.
5. **cert-manager** emite el cert TLS de Let's Encrypt.

Verificar:

```powershell
kubectl -n miproposito get pods
kubectl -n miproposito get svc
kubectl -n miproposito get ingress
kubectl -n miproposito get certificate
```

Todos los pods deberían estar `Running` (1/1) o (2/2 con sidecar CSI).

---

## 10. Verificación post-instalación

```powershell
# Smoke test (sin auth)
.\deploy\smoke-test.ps1 -BaseURL "https://api.miproposito.ucsp.edu.pe"

# Smoke test completo (incluye login/refresh/logout)
$env:SMOKE_ADMIN_EMAIL    = "admin@ucsp.edu.pe"
$env:SMOKE_ADMIN_PASSWORD = "<password-del-seed>"
.\deploy\smoke-test.ps1 -BaseURL "https://api.miproposito.ucsp.edu.pe"
```

---

## 11. Updates posteriores

```powershell
git pull
cd deploy/helm/miproposito
helm dependency build
helm upgrade miproposito . `
  -f values.production.yaml `
  --set global.imageTag=<NUEVO_TAG> `
  -n miproposito
```

Las migraciones SQL pendientes se aplican automáticamente como
`pre-upgrade` hooks. Si una migración ya aplicada fue editada, el upgrade
**aborta** en lugar de corromper la BD (checksum SHA-256 en `__schema_migrations`).

---

## 12. Mapa de servicios y puertos internos

| Servicio              | Puerto gRPC | Puerto HTTP    | Imagen(es)                                     |
|-----------------------|-------------|----------------|-------------------------------------------------|
| users-service         | 50051       | —              | `users-service`, `users-service-migrate`       |
| exams-service         | 50052       | —              | `exams-service`, `exams-service-migrate`       |
| keys-service          | 50053       | —              | `keys-service`, `keys-service-migrate`         |
| hubspot-service       | 50054       | 8080 (webhook) | `hubspot-service`, `hubspot-service-worker`    |
| satisfaction-service  | 50055       | —              | `satisfaction-service`, `satisfaction-service-migrate` |
| analytics-service     | 50056       | —              | `analytics-service`                             |
| **gateway**           | —           | **8080** (Ingress) | `gateway`                                  |

Solo el **gateway** está expuesto al exterior (vía Ingress NGINX +
Let's Encrypt). Los demás son internos al cluster.

---

## 13. Troubleshooting

| Síntoma                                       | Causa probable                                | Solución                                                 |
|-----------------------------------------------|-----------------------------------------------|-----------------------------------------------------------|
| Pods `CreateContainerConfigError`             | SecretProviderClass no resuelve secretos      | `kubectl describe pod ...` → revisar permisos UAMI sobre Key Vault |
| Pods `CrashLoopBackOff` después del migrate   | DB no reachable o credenciales malas          | `kubectl logs <pod>` → revisar firewall del SQL Server (`AllowAllAzureServices`) |
| Job `*-migrate` falla                         | SQL syntax error en migración                 | `kubectl logs job/<name>` → leer error T-SQL exacto       |
| Ingress sin TLS                               | cert-manager negociando                       | Esperar 2-3 min, luego `kubectl describe certificate`     |
| `helm install` falla "version mismatch" deps  | `helm dependency build` no se corrió         | Re-ejecutar `helm dependency build`                       |
| 502 Bad Gateway en `/api/*`                   | gateway no puede dialear los upstreams        | `kubectl logs deploy/<rel>-gateway` → revisar `*_SERVICE_ADDR` |
| 401 inesperado en endpoints públicos          | Endpoint no está en JWT skip-list             | Revisar `gateway/cmd/server/main.go` `jwtSkip[]`          |

---

## 14. Desinstalación

```powershell
helm uninstall miproposito -n miproposito
# (las BDs Azure SQL siguen vivas — esto borra solo lo de k8s)

# Para borrar TODA la infra (irreversible):
az group delete -n rg-miproposito --yes --no-wait
```

---

## 15. CI/CD automático

Después del primer despliegue manual, el pipeline `azure-pipelines.yml`
toma el control:
- En cada push a `main` con cambios en cualquier `<servicio>_service/**`,
  ejecuta `go test`, build de imágenes con tag = build ID, push a ACR y
  `helm upgrade` con approval manual en `Environments → miproposito-prod`.
- Las migraciones T-SQL pendientes se aplican automáticamente
  (`pre-upgrade` Helm hooks).

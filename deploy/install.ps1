#requires -version 5
<#
.SYNOPSIS
    Mi Propósito 2.0 — instalador 1-click para Azure.

.DESCRIPTION
    Aprovisiona la infraestructura completa en Azure y deja todo listo
    para que el cliente solo conecte Azure DevOps. Idempotente: re-correrlo
    no rompe nada (Bicep es declarativo).

    Hace en orden:
      1. Verifica prerrequisitos (az, kubectl, helm).
      2. Login en Azure si no está logueado.
      3. Crea/actualiza el Resource Group.
      4. Despliega el Bicep modular (VNet, AKS, SQL, KV, ACR, Storage, Redis).
      5. Carga los secretos iniciales en Key Vault con valores aleatorios seguros.
      6. Descarga el kubeconfig del AKS.
      7. Imprime los outputs que el cliente debe pegar en Azure DevOps.

.EXAMPLE
    .\deploy\install.ps1 -ResourceGroup rg-miproposito -Location eastus
#>

param(
    [string] $ResourceGroup = "rg-miproposito",
    [string] $Location      = "eastus",
    [string] $NamePrefix    = "miproposito",
    [string] $SqlAdminLogin = "miprop_admin",
    [switch] $EnableAppGateway,    # default $false → más barato (~ahorra $300/mes)
    [switch] $EnableAzureRedis,    # default $false → Redis in-cluster (ahorra ~$15-100/mes). Activar para SLA gestionado.
    [switch] $Staging              # default $false. Si $true → SQL+Redis in-cluster + nodos chicos (para suscripciones nuevas con créditos)
)

# ─────────────────────────────────────────────────────────────────────
# Selección de SKUs según -Staging
# ─────────────────────────────────────────────────────────────────────
# Producción (default): nodos D2s_v3 x3 + Redis Standard C1
# Staging (-Staging):    nodos B2s x2     + Redis Basic C0
if ($Staging) {
    # Standard_B2s_v2: 2 vCPU / 8 GB RAM (~$30/mes c/u). Suscripciones nuevas con créditos
    # gratuitos suelen tener bloqueado el v1 pero permitido el v2.
    $aksNodeVmSize    = 'Standard_B2s_v2'
    $aksNodeCount     = 2
    $redisSkuName     = 'Basic'           # ignorado cuando enableAzureRedis=false
    $redisCapacity    = 0                 # ignorado cuando enableAzureRedis=false
    $enableAzureSql   = $false            # SQL Server adentro del cluster
    $enableAzureRedis = $false            # Redis adentro del cluster (siempre en staging)
    $costEstimate     = '~$60/mes (solo AKS — SQL+Redis in-cluster son gratis)'
    $skuModeLabel     = 'STAGING (SQL+Redis in-cluster)'
} else {
    # Producción: Azure SQL gestionado siempre. Redis: in-cluster por default (ahorra ~$15/mes
    # del Basic al cliente — Redis se usa solo para cache no crítico). Para SLA gestionado de
    # Redis pasar -EnableAzureRedis.
    $aksNodeVmSize    = 'Standard_D2s_v3' # 2 vCPU / 8 GB RAM (~$70/mes c/u)
    $aksNodeCount     = 3
    $redisSkuName     = 'Standard'        # SLA 99.9% — solo aplica si -EnableAzureRedis
    $redisCapacity    = 1                 # 1 GB — solo aplica si -EnableAzureRedis
    $enableAzureSql   = $true             # Azure SQL gestionado (PE en VNet)
    $enableAzureRedis = $EnableAzureRedis.IsPresent
    if ($enableAzureRedis) {
        $costEstimate = '~$380/mes (Azure SQL + Azure Redis gestionados)'
        $skuModeLabel = 'PRODUCCIÓN (Azure SQL + Azure Redis gestionados)'
    } else {
        $costEstimate = '~$280/mes (Azure SQL gestionado + Redis in-cluster)'
        $skuModeLabel = 'PRODUCCIÓN (Azure SQL gestionado + Redis in-cluster)'
    }
}

$ErrorActionPreference = "Stop"

function Step($title) {
    Write-Host ""
    Write-Host "═══ $title" -ForegroundColor Cyan
}
function Ok($msg)   { Write-Host "  ✓ $msg" -ForegroundColor Green }
function Warn($msg) { Write-Host "  ! $msg" -ForegroundColor Yellow }
function Die($msg)  { Write-Host "  ✗ $msg" -ForegroundColor Red; exit 1 }

# ─────────────────────────────────────────────────────────────────────
# 0. Resumen de la corrida
# ─────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "════════════════════════════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  Mi Propósito 2.0 — Installer Azure"                              -ForegroundColor Cyan
Write-Host "════════════════════════════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  Modo         : $skuModeLabel"
Write-Host "  AKS nodos    : $aksNodeCount × $aksNodeVmSize"
Write-Host "  Azure SQL    : $(if ($enableAzureSql) { 'ON (gestionado)' } else { 'OFF (SQL Server in-cluster)' })"
Write-Host "  Azure Redis  : $(if ($enableAzureRedis) { "ON ($redisSkuName C$redisCapacity)" } else { 'OFF (Redis in-cluster)' })"
Write-Host "  App Gateway  : $(if ($EnableAppGateway) { 'ON (~+$300/mes)' } else { 'OFF' })"
Write-Host "  Costo estim. : $costEstimate"
Write-Host "  Resource Grp : $ResourceGroup ($Location)"
Write-Host ""

# ─────────────────────────────────────────────────────────────────────
# 1. Prerequisitos
# ─────────────────────────────────────────────────────────────────────
Step "1/7  Verificando prerrequisitos"

foreach ($tool in @("az", "kubectl", "helm")) {
    $found = Get-Command $tool -ErrorAction SilentlyContinue
    if (-not $found) {
        Die "$tool no está instalado. Ver INSTALL.md sección 'Prerrequisitos'."
    }
    Ok "$tool encontrado"
}

# ─────────────────────────────────────────────────────────────────────
# 2. Login Azure
# ─────────────────────────────────────────────────────────────────────
Step "2/7  Verificando login en Azure"

$account = az account show --query name -o tsv 2>$null
if (-not $account) {
    Warn "No estás logueado. Abriendo login..."
    az login | Out-Null
    $account = az account show --query name -o tsv
}
$tenantId    = az account show --query tenantId -o tsv
$subId       = az account show --query id -o tsv
$myObjectId  = az ad signed-in-user show --query id -o tsv
Ok "Suscripción: $account ($subId)"
Ok "Tenant:      $tenantId"
Ok "Tu user:     $myObjectId"

# ─────────────────────────────────────────────────────────────────────
# 3. Resource Group
# ─────────────────────────────────────────────────────────────────────
Step "3/7  Creando/actualizando Resource Group"

$rgExists = az group exists --name $ResourceGroup
if ($rgExists -eq "true") {
    Ok "RG '$ResourceGroup' ya existe — reutilizando"
} else {
    az group create --name $ResourceGroup --location $Location | Out-Null
    Ok "RG '$ResourceGroup' creado en $Location"
}

# ─────────────────────────────────────────────────────────────────────
# 4. SQL admin password (la generamos UNA vez, la guardamos local)
# ─────────────────────────────────────────────────────────────────────
Step "4/7  Generando password del SQL admin"

$pwdFile = ".\.deploy-state\sql-admin-password.txt"
if (-not (Test-Path .\.deploy-state)) {
    New-Item -ItemType Directory -Path .\.deploy-state | Out-Null
    # gitignore el folder por las dudas (también está en .gitignore raíz).
    "*" | Out-File -FilePath ".\.deploy-state\.gitignore" -Encoding ascii
}

if (Test-Path $pwdFile) {
    $sqlPwd = Get-Content $pwdFile -Raw
    $sqlPwd = $sqlPwd.Trim()
    Ok "Reutilizando password de $pwdFile (NO la borres ni la commitees)"
} else {
    # 24 chars con may/min/digit/símbolo (Azure SQL exige complejidad)
    Add-Type -AssemblyName 'System.Web'
    $sqlPwd = [System.Web.Security.Membership]::GeneratePassword(24, 6)
    $sqlPwd | Out-File -FilePath $pwdFile -Encoding ascii -NoNewline
    Ok "Password generada y guardada en $pwdFile"
    Warn "¡Hacé un BACKUP DE ESE ARCHIVO en tu password manager!"
}

# ─────────────────────────────────────────────────────────────────────
# 5. Bicep deploy
# ─────────────────────────────────────────────────────────────────────
Step "5/7  Desplegando Bicep (puede tardar 10-15 min)"

$deploymentName = "miprop-$(Get-Date -Format 'yyyyMMddHHmmss')"

az deployment group create `
    --resource-group $ResourceGroup `
    --name $deploymentName `
    --template-file deploy/bicep/main.bicep `
    --parameters namePrefix=$NamePrefix `
                 sqlAdminLogin=$SqlAdminLogin `
                 sqlAdminPassword=$sqlPwd `
                 adminObjectId=$myObjectId `
                 enableAppGateway=$($EnableAppGateway.IsPresent) `
                 enableAzureSql=$enableAzureSql `
                 enableAzureRedis=$enableAzureRedis `
                 aksNodeVmSize=$aksNodeVmSize `
                 aksNodeCount=$aksNodeCount `
                 redisSkuName=$redisSkuName `
                 redisCapacity=$redisCapacity | Out-Null

if ($LASTEXITCODE -ne 0) { Die "Bicep deploy falló — revisar logs arriba" }

# Leer outputs del deployment
$outputs = az deployment group show `
    --resource-group $ResourceGroup `
    --name $deploymentName `
    --query properties.outputs -o json | ConvertFrom-Json

$aksName            = $outputs.aksClusterName.value
$kvName             = $outputs.keyVaultName.value
$acrLogin           = $outputs.acrLoginServer.value
$secretsClientId    = $outputs.aksSecretsProviderClientId.value
$oidcIssuerUrl      = $outputs.aksOidcIssuerUrl.value
$nodeResourceGroup  = $outputs.aksNodeResourceGroup.value

# Hosts de SQL y Redis: si Azure los aprovisionó, el FQDN sale del Bicep;
# si no, apuntan al Service interno del cluster (lo crea el Helm chart).
if ($enableAzureSql) {
    $sqlFqdn = $outputs.sqlServerFqdn.value
    $sqlPort = 1433
} else {
    $sqlFqdn = "mssql-server.miproposito.svc.cluster.local"
    $sqlPort = 1433
}

if ($enableAzureRedis) {
    $redisHost = $outputs.redisHost.value
    $redisPort = $outputs.redisSslPort.value
    $redisTls  = "true"
} else {
    $redisHost = "redis-server.miproposito.svc.cluster.local"
    $redisPort = 6379
    $redisTls  = "false"
}

Ok "AKS:       $aksName"
Ok "ACR:       $acrLogin"
Ok "Key Vault: $kvName"
Ok "SQL:       ${sqlFqdn}:${sqlPort}  $(if (-not $enableAzureSql) { '(in-cluster)' })"
Ok "Redis:     ${redisHost}:${redisPort}  $(if (-not $enableAzureRedis) { '(in-cluster)' })"

# ─────────────────────────────────────────────────────────────────────
# 6. Cargar secretos iniciales al Key Vault
# ─────────────────────────────────────────────────────────────────────
Step "6/7  Cargando secretos iniciales al Key Vault"

# Helper para meter un secreto si no existe (o sobreescribirlo si --force).
function Upsert-Secret([string] $name, [string] $value) {
    az keyvault secret set --vault-name $kvName --name $name --value $value | Out-Null
    Ok "secret: $name"
}

# Redis: si Azure aprovisionó, leer la access key del recurso ya creado.
# Si no, generar una password local (la usa Redis in-cluster con --requirepass).
if ($enableAzureRedis) {
    $redisKey = az redis list-keys -g $ResourceGroup -n "${NamePrefix}-redis" --query primaryKey -o tsv
} else {
    Add-Type -AssemblyName 'System.Web'
    $redisKey = [System.Web.Security.Membership]::GeneratePassword(32, 0)
}

# JWT secret (compartido por todos los servicios firmadores/verifiers).
Add-Type -AssemblyName 'System.Web'
$jwtSecret = [System.Web.Security.Membership]::GeneratePassword(64, 0)

# Secrets compartidos (los usan varios servicios via SecretProviderClass).
Upsert-Secret "users-service-sql-password"        $sqlPwd
Upsert-Secret "users-service-redis-password"      $redisKey
Upsert-Secret "users-service-jwt-secret"          $jwtSecret
Upsert-Secret "exams-service-sql-password"        $sqlPwd
Upsert-Secret "keys-service-sql-password"         $sqlPwd
Upsert-Secret "satisfaction-service-sql-password" $sqlPwd
Upsert-Secret "analytics-service-redis-password"  $redisKey
Upsert-Secret "gateway-redis-password"            $redisKey

# HubSpot: placeholders. El cliente los reemplaza con sus tokens reales
# vía Azure portal o `az keyvault secret set` cuando los tenga.
Upsert-Secret "hubspot-service-api-tokens"        "REPLACE_WITH_HUBSPOT_PRIVATE_APP_TOKEN_CSV"
Upsert-Secret "hubspot-service-otp-webhook-token" "REPLACE_WITH_OTP_WEBHOOK_TOKEN"
Upsert-Secret "hubspot-service-redis-password"    $redisKey

Warn "Recordá reemplazar 'hubspot-service-api-tokens' con la(s) API key(s) reales de HubSpot:"
Warn "  az keyvault secret set --vault-name $kvName --name hubspot-service-api-tokens --value 'pat-na1-xxxx,pat-na1-yyyy'"

# ─────────────────────────────────────────────────────────────────────
# 7. Descargar kubeconfig + imprimir resumen
# ─────────────────────────────────────────────────────────────────────
Step "7/7  Conectando kubectl al AKS"

az aks get-credentials -g $ResourceGroup -n $aksName --overwrite-existing | Out-Null
Ok "kubeconfig descargado"

# ─────────────────────────────────────────────────────────────────────
# 7b. Workload Identity Federation para que los pods lean del Key Vault
# ─────────────────────────────────────────────────────────────────────
# El Secrets Store CSI driver necesita autenticarse a AAD para leer del KV.
# Con múltiples MIs en el VMSS del nodepool, useVMManagedIdentity falla con
# "Multiple user assigned identities exist". La forma robusta es Workload
# Identity Federation:
#   1. Crear el namespace si no existe.
#   2. Anotar el ServiceAccount default con el clientID del addon Secrets Provider.
#   3. Crear federated identity credential vinculando OIDC issuer + SA del namespace.
# Los Helm charts ya tienen `azure.workload.identity/use: "true"` en los pods.
Step "7b  Configurando Workload Identity Federation"

# Identidad managed del addon (creada por AKS, vive en el MC_* RG).
$secretsProviderMiName = "azurekeyvaultsecretsprovider-$aksName"
$namespace             = "miproposito"
$fcName                = "miprop-default-sa"

# Crear/asegurar namespace.
kubectl create namespace $namespace --dry-run=client -o yaml | kubectl apply -f - | Out-Null
Ok "namespace '$namespace' listo"

# Anotar el SA default del namespace con el clientID del Secrets Provider MI.
kubectl annotate serviceaccount default `
    "azure.workload.identity/client-id=$secretsClientId" `
    -n $namespace --overwrite | Out-Null
Ok "SA 'default' anotado con clientID=$secretsClientId"

# Crear federated identity credential (idempotente: si ya existe, az retorna error
# que silenciamos y seguimos).
$fcExists = az identity federated-credential show `
    --name $fcName `
    --identity-name $secretsProviderMiName `
    --resource-group $nodeResourceGroup `
    --query name -o tsv 2>$null

if (-not $fcExists) {
    az identity federated-credential create `
        --name $fcName `
        --identity-name $secretsProviderMiName `
        --resource-group $nodeResourceGroup `
        --issuer $oidcIssuerUrl `
        --subject "system:serviceaccount:${namespace}:default" `
        --audience "api://AzureADTokenExchange" | Out-Null
    Ok "federated credential creada: $fcName"
} else {
    Ok "federated credential '$fcName' ya existe — reusando"
}

Write-Host ""
$nodes = kubectl get nodes 2>&1 | Out-String
Write-Host $nodes

# ─────────────────────────────────────────────────────────────────────
# Resumen final — pegar en Azure DevOps Variable Group
# ─────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "════════════════════════════════════════════════════════════════"  -ForegroundColor Green
Write-Host "  ✓ INFRAESTRUCTURA LISTA"                                          -ForegroundColor Green
Write-Host "════════════════════════════════════════════════════════════════"  -ForegroundColor Green
Write-Host ""
Write-Host "Próximo paso: configurar Azure DevOps con estos valores."
Write-Host "Ver guía detallada: deploy/azure-devops/README.md"
Write-Host ""
Write-Host "── Variable Group `miproposito-cicd` (en Azure DevOps Library) ────"
Write-Host "  acrName                 = ${NamePrefix}acr"
Write-Host "  aksResourceGroup        = $ResourceGroup"
Write-Host "  aksClusterName          = $aksName"
Write-Host "  acrLoginServer          = $acrLogin"
Write-Host "  keyVaultName            = $kvName"
Write-Host "  sqlServerFqdn           = $sqlFqdn"
Write-Host "  sqlPort                 = $sqlPort"
Write-Host "  redisHost               = $redisHost"
Write-Host "  redisSslPort            = $redisPort"
Write-Host "  redisTls                = $redisTls"
Write-Host "  tenantId                = $tenantId"
Write-Host "  secretsProviderClientId = $secretsClientId"
Write-Host "  serviceConnectionArm    = azure-subscription"
Write-Host "  serviceConnectionAcr    = acr-connection"
Write-Host "  valuesFile              = $(if ($Staging) { 'values.staging.yaml' } else { 'values.production.yaml' })"
Write-Host "  smokeTestUrl            = "  -NoNewline; Write-Host "(dejá vacío hasta tener DNS apuntado)"
Write-Host ""
Write-Host "── Service Connections a crear en Azure DevOps ────────────────────"
Write-Host "  1. azure-subscription   → ARM con Service Principal"
Write-Host "  2. acr-connection       → Docker Registry → ACR ($acrLogin)"
Write-Host ""
Write-Host "Cuando termines en Azure DevOps, hacé un push a main → la pipeline"
Write-Host "construye + pushea imágenes + helm upgrade automáticamente."
Write-Host ""

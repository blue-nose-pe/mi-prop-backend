#requires -version 5
<#
.SYNOPSIS
    Mi Propósito 2.0 — deploy "manual" desde la PC (reemplaza el pipeline mientras
    se aprueba el grant de paralelismo de Azure DevOps en cuentas nuevas).

.DESCRIPTION
    Replica lo que hace el pipeline (`azure-pipelines.yml`) sin necesitar agents
    de Azure Pipelines:

      1. Build de las 11 imágenes Docker via `az acr build` (corre en la nube de
         ACR, no necesita Docker en tu PC).
      2. `helm dependency build` + `helm upgrade --install` con values.staging.yaml.
      3. Imprime los pods, services e ingresses para que verifiques.

    Idempotente: re-correrlo construye nuevas imágenes con tag nuevo y hace el
    upgrade del release Helm. Si falla un build, podés saltarte el build con
    -SkipBuild para reusar el último tag.

.EXAMPLE
    # Build + deploy completo (~30-40 min la primera vez, después ~10 min).
    .\deploy\dev-deploy.ps1

.EXAMPLE
    # Solo helm upgrade reusando el último tag (más rápido si los Dockerfiles no cambiaron).
    .\deploy\dev-deploy.ps1 -SkipBuild
#>

param(
    [string] $ResourceGroup    = "rg-miproposito",
    [string] $AcrName          = "mipropositoacr",
    [string] $AksClusterName   = "aks-miproposito",
    [string] $KeyVaultName     = "miproposito-kv",
    [string] $Namespace        = "miproposito",
    [string] $ValuesFile       = "values.staging.yaml",
    [switch] $SkipBuild
)

$ErrorActionPreference = "Stop"

function Step($title) {
    Write-Host ""
    Write-Host "═══ $title" -ForegroundColor Cyan
}
function Ok($msg)   { Write-Host "  ✓ $msg" -ForegroundColor Green }
function Warn($msg) { Write-Host "  ! $msg" -ForegroundColor Yellow }
function Die($msg)  { Write-Host "  ✗ $msg" -ForegroundColor Red; exit 1 }

# Tag por timestamp — único por corrida.
$imageTag = Get-Date -Format 'yyyyMMddHHmmss'

Write-Host ""
Write-Host "════════════════════════════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  Mi Propósito 2.0 — Deploy manual (sin Azure Pipelines)"          -ForegroundColor Cyan
Write-Host "════════════════════════════════════════════════════════════════" -ForegroundColor Cyan
Write-Host "  Image tag       : $imageTag"
Write-Host "  ACR             : $AcrName"
Write-Host "  AKS cluster     : $AksClusterName"
Write-Host "  Key Vault       : $KeyVaultName"
Write-Host "  Namespace       : $Namespace"
Write-Host "  Values file     : $ValuesFile"
Write-Host "  Skip build      : $($SkipBuild.IsPresent)"
Write-Host ""

# ─────────────────────────────────────────────────────────────────────
# 1. Verificar prerrequisitos
# ─────────────────────────────────────────────────────────────────────
Step "1/5  Verificando prerrequisitos"

foreach ($tool in @("az", "kubectl", "helm")) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
        Die "$tool no está instalado."
    }
    Ok "$tool encontrado"
}

$account = az account show --query name -o tsv 2>$null
if (-not $account) { Die "No estás logueado en Azure. Correr 'az login' primero." }
Ok "Azure: $account"

# Tenant + identidades para los --set del helm.
$tenantId        = az account show --query tenantId -o tsv
$secretsClientId = az aks show -g $ResourceGroup -n $AksClusterName --query addonProfiles.azureKeyvaultSecretsProvider.identity.clientId -o tsv
$acrLoginServer  = az acr show -n $AcrName --query loginServer -o tsv

Ok "Tenant: $tenantId"
Ok "Secrets Provider Client ID: $secretsClientId"
Ok "ACR login server: $acrLoginServer"

# ─────────────────────────────────────────────────────────────────────
# 2. Build & push de imágenes via ACR Build (si no -SkipBuild)
# ─────────────────────────────────────────────────────────────────────
if ($SkipBuild) {
    # Buscar el último tag pushed para reusar.
    Step "2/5  Skip build — buscando último tag en ACR"
    $imageTag = az acr repository show-tags -n $AcrName --repository users-service --orderby time_desc --top 1 -o tsv
    if (-not $imageTag) { Die "No hay imágenes en ACR todavía. Quitar -SkipBuild en la primera corrida." }
    Ok "Reusando tag: $imageTag"
} else {
    Step "2/5  Build & push de 11 imágenes via ACR Build (~25-35 min)"

    # Mapping de servicios a targets del Dockerfile multi-stage.
    # users_service tiene un target bootstrap adicional (Job post-install que crea superadmin).
    $services = [ordered]@{
        users_service        = @('server', 'migrate', 'bootstrap')
        exams_service        = @('server', 'migrate')
        keys_service         = @('server', 'migrate')
        satisfaction_service = @('server', 'migrate')
        hubspot_service      = @('server', 'worker')
        analytics_service    = @('server')
        gateway              = @('server')
    }

    $totalBuilds = ($services.Values | ForEach-Object { $_.Count } | Measure-Object -Sum).Sum
    $current = 0

    foreach ($svc in $services.Keys) {
        $base = $svc -replace '_', '-'    # users_service → users-service
        foreach ($target in $services[$svc]) {
            $current++
            $imgName = if ($target -eq 'server') { "${base}:${imageTag}" } else { "${base}-${target}:${imageTag}" }

            Write-Host ""
            Write-Host "  [$current/$totalBuilds] Building $imgName (target=$target)..." -ForegroundColor Yellow

            az acr build `
                --registry $AcrName `
                --image $imgName `
                --target $target `
                --file "$svc/Dockerfile" `
                $svc

            if ($LASTEXITCODE -ne 0) {
                Die "Falló el build de $imgName. Revisá el Dockerfile y los logs arriba."
            }
            Ok "$imgName pusheado"
        }
    }
}

# ─────────────────────────────────────────────────────────────────────
# 3. Conectar kubectl al cluster + instalar NGINX Ingress si falta
# ─────────────────────────────────────────────────────────────────────
Step "3/5  Conectando kubectl + instalando NGINX Ingress controller"

az aks get-credentials -g $ResourceGroup -n $AksClusterName --overwrite-existing | Out-Null
Ok "kubeconfig actualizado"

$nodes = kubectl get nodes 2>&1 | Out-String
Write-Host $nodes

# NGINX Ingress controller: necesario para que los Ingress (gateway + hubspot-webhook)
# tengan IP pública. Se instala 1 vez por cluster, idempotente.
$ingressInstalled = kubectl get namespace ingress-nginx --ignore-not-found -o name 2>$null
if (-not $ingressInstalled) {
    Write-Host "  Instalando NGINX Ingress controller (~2 min)..." -ForegroundColor Yellow
    helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx 2>&1 | Out-Null
    helm repo update 2>&1 | Out-Null
    helm install ingress-nginx ingress-nginx/ingress-nginx `
        --namespace ingress-nginx --create-namespace `
        --set controller.service.type=LoadBalancer `
        --set controller.publishService.enabled=true `
        --wait --timeout 5m
    if ($LASTEXITCODE -ne 0) { Die "Falló la instalación de ingress-nginx" }
    Ok "NGINX Ingress controller instalado"
} else {
    Ok "NGINX Ingress controller ya instalado"
}

# ─────────────────────────────────────────────────────────────────────
# 4. Helm upgrade
# ─────────────────────────────────────────────────────────────────────
Step "4/5  Helm dependency build + upgrade --install"

Push-Location "deploy/helm/miproposito"
try {
    helm dependency build | Out-Null
    Ok "Dependencies armadas"

    helm upgrade --install miproposito . `
        --namespace $Namespace `
        --create-namespace `
        -f $ValuesFile `
        --set global.imageTag=$imageTag `
        --set global.imageRegistry=$acrLoginServer `
        --set global.keyVault.name=$KeyVaultName `
        --set global.keyVault.tenantId=$tenantId `
        --set global.keyVault.userAssignedIdentityID=$secretsClientId `
        --timeout 10m

    if ($LASTEXITCODE -ne 0) {
        Die "Falló helm upgrade. Revisá los logs."
    }
    Ok "Helm release 'miproposito' aplicado"
}
finally {
    Pop-Location
}

# ─────────────────────────────────────────────────────────────────────
# 5. Verificación final
# ─────────────────────────────────────────────────────────────────────
Step "5/5  Estado del cluster"

Write-Host ""
Write-Host "── Pods ─────────────────────────────────────────────"
kubectl get pods -n $Namespace -o wide
Write-Host ""
Write-Host "── Services ─────────────────────────────────────────"
kubectl get svc -n $Namespace
Write-Host ""
Write-Host "── Ingresses ────────────────────────────────────────"
kubectl get ingress -n $Namespace 2>&1
Write-Host ""

Write-Host "════════════════════════════════════════════════════════════════"  -ForegroundColor Green
Write-Host "  ✓ DEPLOY MANUAL COMPLETADO"                                       -ForegroundColor Green
Write-Host "════════════════════════════════════════════════════════════════"  -ForegroundColor Green
Write-Host ""
Write-Host "Próximos pasos:"
Write-Host "  • Si los pods están en 'ContainerCreating' o 'Init', esperá 30-60s y re-corré:"
Write-Host "      kubectl get pods -n $Namespace"
Write-Host "  • Para ver logs de un servicio:"
Write-Host "      kubectl logs -n $Namespace deploy/miproposito-users-service -f"
Write-Host "  • Para ver el IP público del Ingress (para apuntar DNS):"
Write-Host "      kubectl get svc -n ingress-nginx"
Write-Host ""

# Smoke test post-deploy. Ejecuta una secuencia mínima de calls al
# gateway para verificar que el sistema responde end-to-end:
#
#   1. GET  /health
#   2. POST /api/auth/login (con credenciales seed)
#   3. POST /api/auth/refresh
#   4. POST /api/auth/logout
#
# Falla rápido (exit 1) si cualquier paso devuelve código no esperado.
#
# Uso:
#   .\smoke-test.ps1 -BaseURL "https://api.miproposito.ucsp.edu.pe" `
#                    -AdminEmail "admin@ucsp.edu.pe" `
#                    -AdminPassword "..."

param(
    [Parameter(Mandatory=$true)] [string] $BaseURL,
    [string] $AdminEmail = $env:SMOKE_ADMIN_EMAIL,
    [string] $AdminPassword = $env:SMOKE_ADMIN_PASSWORD
)

$ErrorActionPreference = "Stop"

function Step($name, [scriptblock] $block) {
    Write-Host "→ $name"
    try {
        & $block
        Write-Host "  ✓ ok" -ForegroundColor Green
    } catch {
        Write-Host "  ✗ FAIL: $_" -ForegroundColor Red
        exit 1
    }
}

# ----- 1. health -----
Step "GET /health" {
    $r = Invoke-RestMethod -Uri "$BaseURL/health" -Method GET
    if ($r.status -ne "ok") { throw "expected status=ok, got $($r.status)" }
}

if (-not $AdminEmail -or -not $AdminPassword) {
    Write-Host "Auth checks skipped (set SMOKE_ADMIN_EMAIL / SMOKE_ADMIN_PASSWORD to enable)" -ForegroundColor Yellow
    exit 0
}

# ----- 2. login -----
$script:tokens = $null
Step "POST /api/auth/login" {
    $body = @{ email = $AdminEmail; password = $AdminPassword } | ConvertTo-Json
    $r = Invoke-RestMethod -Uri "$BaseURL/api/auth/login" -Method POST `
        -Body $body -ContentType "application/json"
    if (-not $r.access_token) { throw "no access_token in response" }
    $script:tokens = $r
}

# ----- 3. refresh -----
Step "POST /api/auth/refresh" {
    $body = @{ refresh_token = $script:tokens.refresh_token } | ConvertTo-Json
    $r = Invoke-RestMethod -Uri "$BaseURL/api/auth/refresh" -Method POST `
        -Body $body -ContentType "application/json"
    if (-not $r.access_token) { throw "no access_token after refresh" }
    $script:tokens.refresh_token = $r.refresh_token  # rotado
}

# ----- 4. logout -----
Step "POST /api/auth/logout" {
    $body = @{ refresh_token = $script:tokens.refresh_token } | ConvertTo-Json
    $null = Invoke-RestMethod -Uri "$BaseURL/api/auth/logout" -Method POST `
        -Body $body -ContentType "application/json"
}

Write-Host "`nAll smoke checks passed ✓" -ForegroundColor Green

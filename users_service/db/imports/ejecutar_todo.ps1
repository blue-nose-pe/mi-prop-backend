# Ejecuta todos los scripts SQL del modelo usuarios contra PostgreSQL 18+.
#
# Uso:
#   .\ejecutar_todo.ps1 -Usuario postgres
#   .\ejecutar_todo.ps1 -Usuario postgres -Password "tu_password"
#   .\ejecutar_todo.ps1 -Usuario postgres -Recreate       # borra db_users y la crea de cero
#
# Requiere psql en PATH. Si no esta, agrega:
#   C:\Program Files\PostgreSQL\18\bin

param(
    [string]$Usuario  = "postgres",
    [string]$Password = "",
    [string]$Host_    = "127.0.0.1",
    [int]$Puerto      = 5432,
    [string]$DbAdmin  = "postgres",     # DB inicial para crear db_users
    [string]$DbApp    = "db_users",
    [switch]$Recreate
)

$ErrorActionPreference = "Stop"

# Localiza psql: primero PATH, luego instalacion estandar de Windows.
$psqlPath = (Get-Command psql -ErrorAction SilentlyContinue).Source
if (-not $psqlPath) {
    $candidate = "C:\Program Files\PostgreSQL\18\bin\psql.exe"
    if (Test-Path $candidate) { $psqlPath = $candidate }
}
if (-not $psqlPath) {
    Write-Error "psql no encontrado. Agregalo al PATH o instala PostgreSQL."
    exit 1
}

if ($Password) { $env:PGPASSWORD = $Password }

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

function Invoke-Psql($db, $sqlFile) {
    Write-Host "  -> $(Split-Path -Leaf $sqlFile) (db=$db)" -ForegroundColor Gray
    & $psqlPath -h $Host_ -p $Puerto -U $Usuario -d $db -v ON_ERROR_STOP=1 -f $sqlFile
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Fallo al ejecutar $(Split-Path -Leaf $sqlFile)"
        exit 1
    }
}

# 1) (Opcional) recrear la base de datos desde cero.
if ($Recreate) {
    Write-Host "Recreando base de datos $DbApp..." -ForegroundColor Yellow
    & $psqlPath -h $Host_ -p $Puerto -U $Usuario -d $DbAdmin -v ON_ERROR_STOP=1 `
        -c "DROP DATABASE IF EXISTS $DbApp WITH (FORCE);"
}

# 2) Crear db_users si no existe (evita error al re-ejecutar).
$exists = & $psqlPath -h $Host_ -p $Puerto -U $Usuario -d $DbAdmin -tA `
          -c "SELECT 1 FROM pg_database WHERE datname='$DbApp'"
if ($exists -ne "1") {
    Write-Host "Creando base de datos..." -ForegroundColor Cyan
    Invoke-Psql $DbAdmin (Join-Path $scriptDir "00_create_database.sql")
} else {
    Write-Host "Base $DbApp ya existe; omito 00_create_database.sql" -ForegroundColor DarkGray
}

# 3) Ejecutar 01..08 en db_users, en orden lexicografico.
$archivos = Get-ChildItem -Path $scriptDir -Filter "*.sql" |
            Where-Object { $_.Name -notlike "00_*" } |
            Sort-Object Name

Write-Host "Aplicando tablas y seed en $DbApp..." -ForegroundColor Cyan
foreach ($f in $archivos) {
    Invoke-Psql $DbApp $f.FullName
}

Write-Host "Listo. Todos los scripts se ejecutaron correctamente." -ForegroundColor Green

# Run all SQL scripts for exams DB in order.
# Usage: .\ejecutar_todo.ps1 -Usuario root -Password "tu_password"

param(
    [string]$Usuario = "root",
    [string]$Password = ""
)

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$archivos = Get-ChildItem -Path $scriptDir -Filter "*.sql" | Sort-Object Name

foreach ($f in $archivos) {
    Write-Host "Ejecutando: $($f.Name)"
    if ($Password) {
        Get-Content $f.FullName -Raw | & mysql -u $Usuario -p$Password
    } else {
        Get-Content $f.FullName -Raw | & mysql -u $Usuario -p
    }
    if ($LASTEXITCODE -ne 0) { Write-Warning "Error en $($f.Name)"; exit 1 }
}

Write-Host "Todos los scripts se ejecutaron correctamente."

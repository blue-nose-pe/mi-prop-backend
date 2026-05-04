# Acceso a la base de datos

Guía corta para conectarse al SQL Server desde una herramienta gráfica
(SSMS, Azure Data Studio, DBeaver, DataGrip, TablePlus, etc.).

## ¿Cuál SQL Server estás usando?

Depende del modo de instalación:

| Modo | SQL Server | Cómo se conecta |
|---|---|---|
| **Staging in-cluster** *(`install.ps1 -Staging`)* | Pod `mssql-server` adentro del AKS | `kubectl port-forward` (ver abajo) |
| **Producción** *(`install.ps1` sin flags)* | Azure SQL Server gestionado con Private Endpoint | VPN corporativa con DNS apuntando al PE |

---

## Staging — `kubectl port-forward`

Solo válido para suscripciones que no tienen Azure SQL gestionado. **Nunca
exponer este SQL al internet.** El port-forward es un túnel local que solo
existe mientras el comando corre en tu terminal.

### 1) Abrir el túnel

```powershell
kubectl port-forward -n miproposito svc/mssql-server 1433:1433
```

Salida esperada:

```
Forwarding from 127.0.0.1:1433 -> 1433
Forwarding from [::1]:1433 -> 1433
```

Dejar la terminal abierta. Cuando la cerrés, el túnel se corta.

### 2) Sacar el SA password

Generado por `install.ps1` y guardado localmente + en Key Vault:

```powershell
# Local (más rápido):
Get-Content .\.deploy-state\sql-admin-password.txt

# Desde Key Vault (idéntico valor):
az keyvault secret show --vault-name miproposito-kv `
  --name users-service-sql-password --query value -o tsv
```

### 3) Conectar con la herramienta gráfica

| Campo | Valor |
|---|---|
| Server / Host | `localhost,1433` (SSMS) o `localhost:1433` (otros) |
| Authentication | SQL Server Authentication |
| User | `sa` |
| Password | el del paso 2 |
| Trust Server Certificate | **Sí** *(el cert del container es self-signed)* |
| Database inicial | `master` o cualquiera de las 4 |

### Bases de datos disponibles

- `db_users` — usuarios, roles, permisos, JWT, audit log
- `db_exams` — tests vocacional/simulacro/hábitos, attempts
- `db_keys` — códigos de acceso a tests
- `db_satisfaction` — encuestas NPS post-test

---

## Producción — Azure SQL gestionado con Private Endpoint

En la cuenta del cliente UCSP, `install.ps1` aprovisiona un Azure SQL Server
con `publicNetworkAccess: Disabled`. Solo accesible vía Private Endpoint en
la VNet del AKS.

Para conectarse desde una PC del equipo del cliente:

1. **Estar dentro de la VNet** del cliente (VPN corporativa, Bastion, o
   Application Gateway si lo activaron).
2. Resolver el FQDN privado: `<namePrefix>-sql.privatelink.database.windows.net`.
3. Conectar con el password del Key Vault (`users-service-sql-password`).

Si necesitás conectarte desde una PC fuera de la VNet (caso debug remoto):

1. Pedir al admin que te conecte a la VPN, o
2. Levantar un jumphost en la VNet privada (Azure Bastion + VM).

**Nunca** habilitar `publicNetworkAccess: Enabled` en el SQL Server de
producción.

---

## Acceso desde un Pod del cluster (alternativa rápida)

Útil para queries one-off sin abrir herramientas gráficas:

```powershell
$pwd = az keyvault secret show --vault-name miproposito-kv `
  --name users-service-sql-password --query value -o tsv

kubectl run --rm -it --restart=Never --image=mcr.microsoft.com/mssql-tools `
  sqlclient -n miproposito -- `
  /opt/mssql-tools/bin/sqlcmd `
    -S mssql-server -U sa -P $pwd `
    -d db_users `
    -Q "SELECT TOP 10 * FROM users"
```

Esto crea un Pod efímero, ejecuta sqlcmd, y se autodestruye al terminar
(`--rm`). El password viaja entre tu PC y el cluster vía la Kubernetes API,
no expone el SQL al internet.

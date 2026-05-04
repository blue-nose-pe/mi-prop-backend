# Configuración de Azure DevOps — Mi Propósito 2.0

Esta guía deja Azure DevOps listo para que **cada `git push` a `main`**
construya las imágenes Docker, las suba a Azure Container Registry, y haga
`helm upgrade` sobre el clúster AKS automáticamente (con aprobación manual
antes del deploy productivo).

> **Antes de empezar**, ya tenés que haber corrido `deploy/install.ps1`
> exitosamente. Ese script imprimió al final una sección que dice
> *"Variable Group `miproposito-cicd`"* — vas a copiar esos valores acá.

Tiempo estimado: **15–20 minutos** la primera vez.

---

## Resumen de lo que vas a crear

| # | Recurso en Azure DevOps         | Para qué sirve                                          |
|---|---------------------------------|---------------------------------------------------------|
| 1 | Project                         | Contenedor del repo + pipelines                          |
| 2 | Repository (Azure Repos)        | Código del backend                                       |
| 3 | Service Connection: **azure-subscription** | Para `az` y `helm upgrade` (también sirve para AKS) |
| 4 | Service Connection: **acr-connection** | Push de imágenes Docker al ACR                  |
| 5 | Variable Group: **miproposito-cicd** | Variables compartidas (nombres de recursos)         |
| 6 | Environment: **miproposito-prod** | Aprobación manual antes de cada deploy                |

> **Nota**: solo necesitamos 2 Service Connections. Una sola conexión ARM
> (`azure-subscription`) cubre `az aks get-credentials` + `helm upgrade`,
> y la conexión Docker (`acr-connection`) hace el push de imágenes.

---

## Fase 1 — Crear la organización y el proyecto (solo si arrancás de cero)

1. Andá a https://dev.azure.com → **New organization**
   - Nombre: `bluenose-ucsp` (o el que prefieras)
   - Región: **South Brazil** (la más cercana a Perú con baja latencia)
2. Una vez adentro: **+ New project**
   - Nombre: `mi-proposito`
   - Visibility: **Private**
   - Version control: **Git**
   - Work item process: **Agile**
3. Click *Create project*.

> Si ya tenés organización/proyecto creados, saltá a la siguiente fase.

---

## Fase 2 — Subir el código a Azure Repos

Ya tenés un repo local. Hay que apuntarlo al remote de Azure Repos.

### 2.1 Obtener la URL del remote

En Azure DevOps: **Repos → Files → Clone** (botón arriba a la derecha) →
copia la URL HTTPS. Se ve así:

```
https://dev.azure.com/bluenose-ucsp/mi-proposito/_git/mi-proposito
```

### 2.2 Apuntar tu repo local

```powershell
# Si todavía no lo tenés inicializado:
git init
git add .
git commit -m "feat: initial commit Mi Proposito 2.0"

# Apuntar al remote de Azure DevOps:
git remote add origin https://dev.azure.com/bluenose-ucsp/mi-proposito/_git/mi-proposito
git branch -M main
git push -u origin main
```

> La primera vez te va a pedir credenciales. Usá un **Personal Access
> Token** (se crea desde el ícono de usuario arriba a la derecha →
> *Personal access tokens* → *New token* → permisos: **Code (Read & write)**).

---

## Fase 3 — Crear las 2 Service Connections

Las Service Connections son las credenciales que el pipeline usa para
hablar con Azure. Andá a **Project settings** (ícono de engranaje abajo
a la izquierda) → **Service connections** → **New service connection**.

### 3.1 `azure-subscription` (ARM con Service Principal)

1. Tipo: **Azure Resource Manager** → *Next*.
2. Auth method: **Service principal (automatic)** → *Next*.
3. Scope level: **Subscription**.
4. Subscription: la tuya (debería autocompletar).
5. Resource group: **(dejar vacío)** — necesitamos scope a la suscripción
   completa, no solo a un RG.
6. Service connection name: **`azure-subscription`** *(exacto, sin espacios)*.
7. Marcá **Grant access permission to all pipelines**.
8. Click *Save*.

> Esto crea automáticamente un Service Principal en tu Azure AD con
> rol **Contributor** sobre la suscripción. Si tu org tiene políticas
> estrictas, el admin puede tener que aprobarlo.

### 3.2 `acr-connection` (Docker Registry)

1. Tipo: **Docker Registry** → *Next*.
2. Registry type: **Azure Container Registry**.
3. Authentication Type: **Service Principal**.
4. Subscription: la tuya.
5. Azure container registry: `mipropositoacr` *(o el nombre que imprimió `install.ps1`)*.
6. Service connection name: **`acr-connection`** *(exacto)*.
7. Marcá *Grant access permission to all pipelines*.
8. Click *Save*.

> **No necesitás una Service Connection de Kubernetes** — el pipeline
> usa `az aks get-credentials` desde la conexión ARM, que es más simple
> y se renueva sola.

---

## Fase 4 — Crear el Variable Group `miproposito-cicd`

Andá a **Pipelines → Library → + Variable group**.

| Campo                   | Valor                                          |
|-------------------------|------------------------------------------------|
| Variable group name     | `miproposito-cicd` *(exacto)*                  |

Después agregá las siguientes variables. **Los valores los imprimió
`install.ps1` al final** — buscalos en la consola y copialos:

| Variable                  | Ejemplo                              | ¿Secreto? |
|---------------------------|--------------------------------------|-----------|
| `acrName`                 | `mipropositoacr`                     | No        |
| `acrLoginServer`          | `mipropositoacr.azurecr.io`          | No        |
| `aksResourceGroup`        | `rg-miproposito`                     | No        |
| `aksClusterName`          | `aks-miproposito`                    | No        |
| `keyVaultName`            | `kv-miproposito-xxxx`                | No        |
| `sqlServerFqdn`           | `sql-mipropositoxxx.privatelink.database.windows.net` | No |
| `redisHost`               | `miproposito-redis.redis.cache.windows.net` | No |
| `redisSslPort`            | `6380`                               | No        |
| `tenantId`                | `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx` | No      |
| `secretsProviderClientId` | `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx` | No      |
| `serviceConnectionArm`    | `azure-subscription`                 | No        |
| `serviceConnectionAcr`    | `acr-connection`                     | No        |

> Tip: si perdiste la salida de `install.ps1`, podés recuperar todo con:
> ```powershell
> az deployment group show -g rg-miproposito `
>   --name (az deployment group list -g rg-miproposito --query "[0].name" -o tsv) `
>   --query properties.outputs
> ```

Click *Save* arriba a la derecha.

---

## Fase 5 — Crear el Environment `miproposito-prod` con aprobación manual

El Environment es el "destino" de deploy. Le agregamos una **manual
approval gate** para que ningún push accidental rompa producción.

1. **Pipelines → Environments → + New environment**.
2. Name: **`miproposito-prod`** *(exacto)*.
3. Resource: **None** (lo dejamos vacío, no es necesario aquí).
4. Click *Create*.
5. Una vez creado, click en los **3 puntos arriba a la derecha** →
   **Approvals and checks** → **+ Add check**.
6. Elegí **Approvals**:
   - Approvers: **vos mismo + Pablo** *(podés agregar más después)*.
   - Minimum number of approvers: `1`.
   - Timeout: `30 days`.
7. Click *Create*.

---

## Fase 6 — Habilitar el pipeline `azure-pipelines.yml`

1. **Pipelines → Pipelines → + New pipeline**.
2. Where is your code: **Azure Repos Git**.
3. Repository: **mi-proposito**.
4. Configure: **Existing Azure Pipelines YAML file**.
5. Branch: `main`. Path: `/azure-pipelines.yml`.
6. Click *Continue*.
7. En la pantalla de review: click la flechita ▼ al lado de *Run* →
   **Save** (sin correr todavía).

> El pipeline ya está configurado para gatillarse automáticamente con
> cada push a `main`.

---

## Fase 7 — Verificar que todo funciona

Hacé un cambio mínimo en el repo (ej: editá el `README.md` raíz) y
empujá:

```powershell
git add README.md
git commit -m "chore: trigger first pipeline run"
git push
```

Andá a **Pipelines → Pipelines → mi-proposito** y deberías ver el run
arrancando. Stages que se ejecutan:

```
test  →  build  →  push  →  deploy (con aprobación manual)
```

Cuando llegue al stage `deploy`, vas a ver una notificación de
**Pending approval**. Click *Review → Approve*.

El `helm upgrade` corre y cuando termine podés verificar:

```powershell
kubectl get pods -n miproposito
```

Deberías ver todos los pods en estado `Running`.

---

## Troubleshooting

### El pipeline falla en `test` con `permission denied`
La imagen del agente Linux por defecto no tiene Go instalado en la
versión correcta. Está bien — el step `UseGoTool@0` la instala.
Si falla, verificá que en `azure-pipelines.yml` la variable
`goVersion` esté en `1.24`.

### El pipeline falla en `push` con `unauthorized`
La Service Connection `acr-connection` no tiene permisos. Andá a
**Project settings → Service connections → acr-connection → Edit →
Manage Service Principal** y verificá que tenga rol **AcrPush** sobre
el ACR.

### El pipeline falla en `deploy` con `error: You must be logged in to the server`
La Service Connection ARM (`azure-subscription`) perdió permisos sobre
el cluster. Verificá que el Service Principal tiene rol **Azure
Kubernetes Service Cluster User Role** sobre el RG `rg-miproposito`:
```powershell
$sp = az ad sp list --display-name "<nombre-del-service-principal>" --query "[0].appId" -o tsv
az role assignment create --assignee $sp `
  --role "Azure Kubernetes Service Cluster User Role" `
  --scope (az group show -n rg-miproposito --query id -o tsv)
```

### El stage `deploy` queda esperando para siempre
La aprobación manual está pendiente. Andá a la pipeline run y aprobá.

### El pod del servicio queda en `CrashLoopBackOff`
Mirá los logs: `kubectl logs -n miproposito <pod-name> --previous`.
Causa habitual: secret faltante en Key Vault. Revisá que `install.ps1`
haya creado todos los secretos:
```powershell
az keyvault secret list --vault-name <kvName> --query "[].name" -o tsv
```

---

## Próximos pasos opcionales

- **Branch policies**: protegé `main` para requerir PR + 1 reviewer
  + pipeline verde. *(Repos → Branches → main → Branch policies)*.
- **Pull Request validation**: el pipeline ya corre en PRs (sección
  `pr:` del YAML); marcá la policy *"Build validation"* para
  bloquear merges con tests rojos.
- **Boards**: configurá el work item process Agile para trackear
  bugs y features (gratis, viene incluido).
- **Wiki**: documentá runbooks de incidentes, decisiones de arquitectura
  y onboarding de nuevos devs.

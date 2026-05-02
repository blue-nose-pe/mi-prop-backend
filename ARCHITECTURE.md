# Arquitectura canónica — Mi Propósito 2.0

**Este documento es la fuente de verdad para CÓMO se construye cada microservicio.** Aplica a `users_service`, `exams_service`, `keys_service`, `hubspot_service`, `satisfaction_service`, `analytics_service` y `gateway`. Si un servicio se desvía, debe justificarse explícitamente en su propio `ARCHITECTURE.md` local.

Los principios son:

- **Arquitectura hexagonal** (Ports & Adapters) — el dominio nunca depende de detalles de infraestructura.
- **CQRS ligero** — los casos de uso que mutan estado (Commands) están separados de los que leen estado (Queries). Comparten el mismo modelo y la misma BD; la separación es a nivel de código, no de almacenamiento.
- **SOLID** — cada interfaz pequeña y cohesiva, dependencias inyectadas por constructor, una responsabilidad por struct, cambios abiertos a extensión y cerrados a modificación.
- **Clean code** — nombres explícitos en español/inglés según convención local, funciones cortas, sin comentarios obvios, errores tipados, sin lógica en `main.go` excepto wiring.

## 1. Estructura de carpetas — idéntica en todos los servicios

```
<servicio>_service/
├── cmd/
│   ├── server/main.go              # punto de entrada del servidor gRPC
│   └── migrate/main.go             # binario que aplica migraciones T-SQL (k8s Job)
├── config/
│   └── config.go                   # carga variables de entorno
├── db/
│   ├── imports/                    # schemas legacy de referencia (Postgres/MySQL — solo durante el porteo)
│   └── migrations/                 # archivos T-SQL numerados (001_xxx.sql, 002_xxx.sql, ...) embed via go:embed
├── internal/
│   ├── core/
│   │   ├── domain/                 # entidades + value objects + errores tipados (NO depende de nada externo)
│   │   ├── command/                # ⬅ CQRS: casos de uso que MUTAN (Create, Update, Delete, Assign, Login...)
│   │   ├── query/                  # ⬅ CQRS: casos de uso que LEEN (Get, Search, List, Has...)
│   │   └── ports/
│   │       ├── inbound.go          # interfaces Commands/Queries que el core OFRECE
│   │       └── outbound.go         # interfaces Repository/Cache/Hasher que el core NECESITA
│   ├── adapters/
│   │   ├── inbound/
│   │   │   └── grpc/               # gRPC server + handlers (mappers proto ↔ dominio)
│   │   └── outbound/
│   │       ├── mssql/              # repositorios contra Azure SQL
│   │       ├── redis/              # cache
│   │       └── <otros>/            # bcrypt, jwt, audit-sink, hubspot-client, etc.
│   └── shared/                     # apperr, search, jwtmw, auditmw, mssql, migrator (cada servicio tiene SU copia — autonomía)
├── proto/
│   ├── *.proto                     # contrato del servicio
│   ├── common/*.proto              # error.proto, search.proto, audit.proto (copia local)
│   └── gen/                        # código generado por buf (no editar a mano)
├── ARCHITECTURE.md                 # decisiones específicas del servicio (link a este doc)
├── PLAYBOOK.md                     # recetas de "cómo agregar X" en este servicio
├── Dockerfile
├── go.mod
├── go.sum
└── .env.example
```

## 2. Las 7 reglas de oro

Inviolables. Aplican a todos los servicios.

1. **Dependencias apuntan al core.** El core (`internal/core/`) NUNCA importa `mssql`, `redis`, `grpc`, `bcrypt`, `jwt`, `bullmq`. Si lo hace, el patrón está roto.
2. **Commands y Queries están separados físicamente.** No hay un solo struct que tenga `Create()` y `Get()` juntos. Distintos archivos, distintas estructuras, distintos puertos inbound (`UserCommands`, `UserQueries`).
3. **Errores son `*apperr.Error` tipados** vía `internal/shared/apperr/`. Nunca `errors.New("...")` para errores que llegan al cliente. Definidos en `domain/errors.go`.
4. **Handler gRPC es tonto.** Traduce proto ↔ DTO de comando/query, llama al port inbound, traduce resultado ↔ proto. Cero validación o lógica de negocio.
5. **Toda columna expuesta por búsqueda DEBE estar en un schema** (`*_schema.go` en `adapters/outbound/mssql/`). Whitelist explícita. `password_hash` o cualquier campo sensible no se declara → no se puede filtrar/devolver.
6. **`cmd/server/main.go` es el ÚNICO lugar que construye cosas concretas.** Todo lo demás recibe interfaces por constructor. Ni `sql.Open()` ni `redis.NewClient()` fuera de ahí.
7. **Cada servicio es autónomo.** No comparte código Go con otros servicios. Si otro servicio necesita la misma utilidad (ej. `migrator`, `mssql`), copia el archivo. La duplicación se acepta a cambio de independencia total de release y dependencias.

## 3. CQRS — qué va a `command/` y qué a `query/`

| Tipo | Características | Va a |
|---|---|---|
| **Command** | Muta estado (INSERT/UPDATE/DELETE), invalida cache, registra auditoría, dispara eventos | `internal/core/command/` |
| **Query** | Solo lee, idempotente, sin efectos secundarios, puede usar cache agresivamente | `internal/core/query/` |

**Regla de oro CQRS**: si un método tiene en su cuerpo un `INSERT`, `UPDATE`, `DELETE`, un `cache.Delete`, un `audit.Record`, o emite un evento → es un command. Si solo lee → query.

Casos límite:
- `Authenticate / Login`: COMMAND. Aunque lee credenciales, también actualiza `last_access_at` y emite tokens.
- `Search`: QUERY. Lee con filtros, no muta.
- `IncrementUsageCounter`: COMMAND. Aunque parece lectura por su nombre, muta el contador.

### Convención de nombres de archivos

Un archivo por *facet* (área temática). NO un archivo por método. Ejemplos en `users_service`:

```
internal/core/command/
├── user.go         # Create, Update, Deactivate
├── auth.go         # Authenticate, Login, Refresh, Logout
└── permission.go   # AssignGroup, RevokeGroup

internal/core/query/
├── user.go         # Get, GetByEmail, Search
└── permission.go   # ListUserPermissions, HasPermission
```

### Convención de naming de DTOs

- Input de command: `<Verbo><Entidad>Input` (ej. `CreateUserInput`, `LoginInput`).
- Output de command: `<Verbo><Entidad>Output` cuando devuelve estructura compleja, o directamente `*domain.<Entidad>` cuando es la entidad.
- Input de query: `Get<Entidad>Input`, `Search<Entidad>Input`.

## 4. Ports — interfaces inbound y outbound

### Inbound (lo que el core OFRECE al mundo)

Una interfaz **por facet**, agrupada en `Commands` o `Queries`. Mantenerlas pequeñas (3-5 métodos cada una). Ejemplo (`users_service/internal/core/ports/inbound.go`):

```go
type UserCommands interface {
    Create(ctx context.Context, in CreateUserInput) (*domain.User, error)
    Update(ctx context.Context, in UpdateUserInput) (*domain.User, error)
    Deactivate(ctx context.Context, id domain.UserID) error
}

type UserQueries interface {
    Get(ctx context.Context, id domain.UserID) (*domain.User, error)
    GetByEmail(ctx context.Context, email domain.Email) (*domain.User, error)
    Search(ctx context.Context, req search.Request) (*search.Response, error)
}

type AuthCommands interface {
    Authenticate(ctx context.Context, in AuthenticateInput) (*AuthenticateOutput, error)
    // Login, Refresh, Logout cuando se agreguen JWT
}

type PermissionCommands interface {
    AssignGroup(ctx context.Context, userID domain.UserID, groupID uint32) error
    RevokeGroup(ctx context.Context, userID domain.UserID, groupID uint32) error
}

type PermissionQueries interface {
    ListUserPermissions(ctx context.Context, userID domain.UserID) ([]string, error)
    HasPermission(ctx context.Context, userID domain.UserID, code string) (bool, error)
}
```

### Outbound (lo que el core NECESITA del mundo)

Una interfaz por dependencia técnica, definida en `outbound.go`. Ejemplos:

```go
type UserRepository interface {
    Save(ctx context.Context, u *domain.User) (domain.UserID, error)
    Update(ctx context.Context, u *domain.User) error
    FindByID(ctx context.Context, id domain.UserID) (*domain.User, error)
    FindByEmail(ctx context.Context, email domain.Email) (*domain.User, error)
    ExistsByDocument(ctx context.Context, doc string) (bool, error)
    SetActive(ctx context.Context, id domain.UserID, active bool) error
    TouchLastAccess(ctx context.Context, id domain.UserID) error
    Search(ctx context.Context, req search.Request) (*search.Response, error)
}

type UserCache interface { ... }
type PasswordHasher interface { ... }
type PermissionRepository interface { ... }
```

## 5. Orden de trabajo (igual al PLAYBOOK del users_service)

Cuando agregas algo nuevo, sigue **siempre** este orden:

```
  1. PROTO       → contrato gRPC
        ↓
  2. DOMAIN      → entidades + errores
        ↓
  3. PORTS       → interfaces inbound/outbound
        ↓
  4. COMMAND/QUERY → lógica de negocio (decide cuál según CQRS)
        ↓
  5. ADAPTERS OUT → mssql, redis, ... (implementan ports outbound)
        ↓
  6. ADAPTERS IN  → grpc handler (consume ports inbound)
        ↓
  7. MAIN        → wiring (cmd/server/main.go)
        ↓
  8. BUILD + TEST
```

Cada capa declara contratos antes de que la siguiente los use.

## 6. Escalabilidad — cómo se decide

- **Stateless por defecto.** Ningún servicio guarda estado en memoria local que afecte correctness. Todo va a Azure SQL o Redis. Permite N réplicas detrás de un Service.
- **HPA** (Horizontal Pod Autoscaler) en cada Helm chart con CPU/memoria como métrica base. Servicios I/O-heavy (hubspot-service worker) usan KEDA con métrica de longitud de cola.
- **Pool de conexiones** Azure SQL via `pkg/mssql/`: defaults `MaxOpenConns=25, MaxIdleConns=5, ConnMaxLifetime=30m`.
- **Caché en queries**, NO en commands. Commands invalidan agresivamente.
- **Read-replicas Azure SQL** se contemplan a futuro (no fase inicial). Cuando lleguen, los queries leerán de réplica y los commands del primary; CQRS lo facilita.
- **gRPC entre servicios** con keepalive y deadlines de cliente. NO HTTP entre microservicios.

## 7. SOLID aplicado — checklist por commit

- **S — Single Responsibility**: ¿este struct/función hace UNA sola cosa? Si su nombre tiene "And" o el cuerpo cambia de contexto a media página, partir.
- **O — Open/Closed**: ¿agregar una nueva regla requiere abrir varios archivos? Si sí, posiblemente falta una nueva interfaz outbound (extensión por composición, no por modificación).
- **L — Liskov**: ¿una implementación nueva del puerto rompe contratos del existente (panics, retornos NULL inesperados, métodos que ahora bloquean)? Definir el contrato en doc del port y respetarlo.
- **I — Interface Segregation**: ¿el handler depende de una interfaz con 15 métodos donde usa 3? Partir la interfaz.
- **D — Dependency Inversion**: ¿el core importa un paquete concreto (`mssql`, `redis`, `pgx`)? Mover detrás de un puerto.

## 8. Clean code — convenciones

- **Idioma**: comentarios y docs en **español**. Nombres de tipos/funciones públicos en inglés (siguen convención Go). DTOs/comandos también en inglés (`CreateUserInput`, no `CrearUsuarioInput`).
- **Comentarios**: solo cuando agregan información NO obvia (constraint del negocio, workaround, decisión histórica). NO repetir el código en prosa.
- **Funciones cortas**: si una función pasa de ~50 líneas, usualmente está haciendo dos cosas.
- **Errores tipados** (`apperr`): nunca `errors.New`/`fmt.Errorf` para errores que viajan al cliente. Sí para envolver errores internos al loggear.
- **No `if err != nil` anidado**: usar early return.
- **Tests**: cada command/query con un test unitario que use mocks de los outbound ports. Tests de integración con `testcontainers-go` levantando SQL Server.
- **Sin imports innecesarios**: `goimports -w` antes de commit.

## 9. Anti-patrones (qué NO hacer)

- ❌ Mezclar Command y Query en el mismo struct/archivo.
- ❌ Que un Query escriba en BD o invalide cache.
- ❌ Que un Command no registre en `audit_log` cuando muta entidades sensibles.
- ❌ Importar `database/sql` o `*pgx*` desde `internal/core/`.
- ❌ Hacer `errors.Is(err, sql.ErrNoRows)` fuera del adapter mssql (la traducción a `domain.ErrXxxNotFound` la hace el adapter).
- ❌ Hardcodear strings de propiedades en el handler (ej `"email"`); pasarlos por DTO desde proto.
- ❌ Hacer wiring fuera de `cmd/server/main.go`.
- ❌ Que el handler gRPC retorne `error` directo: SIEMPRE `apperr.ToGRPC(ctx, err)`.

## 10. Cómo arrancar un nuevo servicio

1. Copiar la estructura de carpetas de `users_service/` (sin código).
2. Crear `go.mod` con módulo `<servicio>_service`.
3. Agregarlo a `go.work` raíz.
4. Copiar `internal/shared/{apperr,search,jwtmw,auditmw,mssql,migrator}` desde `users_service/` (autonomía).
5. Definir su `proto/<servicio>.proto` siguiendo el contrato HubSpot-style + envelope de error.
6. Copiar `proto/common/{error,search}.proto` (locales por servicio).
7. Crear primer Command y primer Query (mínimo `Create<Entity>` + `Get<Entity>`).
8. Implementar adapter `mssql` con un repo dummy.
9. Wiring en `cmd/server/main.go`.
10. `go build ./...` debe pasar limpio.

Después agregar feature por feature siguiendo el [PLAYBOOK.md](users_service/PLAYBOOK.md) del servicio.

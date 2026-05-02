# `users_service` — guía archivo por archivo

Microservicio en **Go** con arquitectura **Hexagonal (Ports & Adapters)**, gRPC hacia afuera, Postgres + Redis hacia adentro, búsqueda estilo HubSpot y envelope de error unificado.

Esta guía camina **cada archivo** del servicio, explica **por qué existe cada línea relevante**, y da **ejemplos visuales**. Léela una vez de arriba a abajo; después puedes saltar a la sección que necesites.

---

## 📑 Índice

1. [Concepto base (30 segundos)](#1-concepto-base-30-segundos)
2. [Flujo visual de una request](#2-flujo-visual-de-una-request)
3. [Estructura de carpetas completa](#3-estructura-de-carpetas-completa)
4. [PROTO — el contrato con el mundo](#4-proto--el-contrato-con-el-mundo)
5. [CORE — la lógica pura](#5-core--la-lógica-pura)
6. [SHARED — utilitarios transversales](#6-shared--utilitarios-transversales)
7. [INBOUND — cómo entran las requests](#7-inbound--cómo-entran-las-requests)
8. [OUTBOUND — cómo hablo con el mundo](#8-outbound--cómo-hablo-con-el-mundo)
9. [COMPOSITION — cómo se arma todo](#9-composition--cómo-se-arma-todo)
10. [Ejemplos visuales completos](#10-ejemplos-visuales-completos)
11. [Cómo extenderlo sin romper nada](#11-cómo-extenderlo-sin-romper-nada)

---

## 1. Concepto base (30 segundos)

### Las 3 ideas que sostienen todo el código

1. **Tres círculos concéntricos.** El código vive en 3 capas:
   - **Core** (centro): pura lógica de negocio. NO conoce Postgres, Redis, gRPC.
   - **Inbound adapters** (izquierda): cómo el exterior te habla. Aquí vive gRPC.
   - **Outbound adapters** (derecha): cómo hablas con el exterior. Aquí viven Postgres, Redis, bcrypt.

2. **El core solo declara qué necesita.** Usa **interfaces** (ports). Los adapters las **implementan**. El core nunca dice "importa pgx"; dice "necesito algo que sepa hacer `Save(user)`".

3. **Regla de oro:** las flechas de dependencia siempre apuntan hacia adentro.
   ```
   adapters  ──▶  core    ✅
   core      ──▶  adapters ❌
   ```

### Visual del hexágono

```
                       ┌──────────────────────────┐
                       │      OUTER WORLD         │
                       └──────────────────────────┘
                                   ▲ ▼
     ┌───────────────────────┐           ┌──────────────────────────┐
     │    INBOUND ADAPTER    │           │    OUTBOUND ADAPTERS     │
     │                       │           │                          │
     │  gRPC Handler         │           │  Postgres (user, perm,   │
     │  (+ interceptors)     │           │             school)      │
     │                       │           │  Redis    (user cache)   │
     │                       │           │  bcrypt   (password)     │
     └──────────┬────────────┘           └────────────▲─────────────┘
                │                                     │
                │         llama          implementa   │
                │        puertos         puertos      │
                ▼                                     │
     ┌──────────────────────────────────────────────────┐
     │                       CORE                        │
     │  ┌───────────────────────────────────────────┐    │
     │  │                 DOMAIN                    │    │
     │  │  User, School, Permission, errors         │    │
     │  └───────────────────────────────────────────┘    │
     │  ┌───────────────────────────────────────────┐    │
     │  │                 PORTS                     │    │
     │  │  inbound (UserUseCase, PermissionUseCase) │    │
     │  │  outbound (UserReader, UserWriter, ...)   │    │
     │  └───────────────────────────────────────────┘    │
     │  ┌───────────────────────────────────────────┐    │
     │  │                 SERVICE                   │    │
     │  │  UserService, PermissionService           │    │
     │  │  (implementa inbound, USA outbound)       │    │
     │  └───────────────────────────────────────────┘    │
     └──────────────────────────────────────────────────┘
```

---

## 2. Flujo visual de una request

Una llamada `CreateUser` va así, de izquierda a derecha:

```
┌────────┐      ┌──────────┐      ┌─────────────┐      ┌──────────────┐
│ Gateway│ gRPC │  Handler │ port │ UserService │ port │ UserRepo(pg) │
│        │─────▶│ (inbound)│─────▶│   (core)    │─────▶│  (outbound)  │
└────────┘      └──────────┘      └─────┬───────┘      └──────────────┘
                                        │
                                        │ port
                                        ├─────▶┌──────────────┐
                                        │      │  UserCache   │
                                        │      │   (redis)    │
                                        │      └──────────────┘
                                        │
                                        │ port
                                        └─────▶┌──────────────┐
                                               │PasswordHasher│
                                               │   (bcrypt)   │
                                               └──────────────┘
```

En qué momento hace qué:

1. **Handler** recibe el mensaje proto, traduce a "comando" del core (`CreateUserCmd`).
2. **UserService** valida email + password (lógica de negocio), chequea duplicado (vía repo), hashea password (vía hasher), guarda (vía repo), cachea (vía cache).
3. **UserRepo (postgres)** es el único que habla SQL real.
4. **UserCache (redis)** guarda JSON en memoria clave-valor.
5. **Hasher (bcrypt)** aplica el algoritmo bcrypt al password.

El core (en el medio) **no sabe** que existe Postgres, Redis, ni bcrypt. Solo usa interfaces.

---

## 3. Estructura de carpetas completa

```
users_service/
├── cmd/
│   └── main.go                                   ← Arranque + composition root
├── config/
│   └── config.go                                 ← ENV vars tipadas
├── proto/
│   ├── user.proto                                ← Contrato del servicio
│   ├── common/
│   │   ├── search.proto                          ← Contrato HubSpot-style
│   │   └── error.proto                           ← Envelope de error
│   └── gen/                                      ← (autogenerado por buf)
│       ├── user.pb.go
│       ├── user_grpc.pb.go
│       └── common/
│           ├── search.pb.go
│           └── error.pb.go
├── internal/
│   ├── core/                                     🧠 Sin deps externas
│   │   ├── domain/
│   │   │   ├── user.go                           ← Entity User + VOs
│   │   │   ├── school.go                         ← Entity School
│   │   │   ├── permission.go                     ← Entities Permission + Group
│   │   │   └── errors.go                         ← Errores de dominio
│   │   ├── ports/
│   │   │   ├── inbound.go                        ← Interfaces que el core OFRECE
│   │   │   └── outbound.go                       ← Interfaces que el core NECESITA
│   │   └── service/
│   │       ├── user_service.go                   ← Casos de uso de usuarios
│   │       └── permission_service.go             ← Casos de uso de permisos
│   ├── shared/                                   🔌 Utilitarios sin infra
│   │   ├── apperr/
│   │   │   ├── errors.go                         ← Error tipado + correlation ID
│   │   │   └── apperr.go                         ← ToGRPC: traductor único
│   │   └── search/
│   │       ├── types.go                          ← Request/Filter/Response
│   │       ├── errors.go                         ← Factories de errores
│   │       └── proto.go                          ← Conversión pb ↔ Go
│   └── adapters/
│       ├── inbound/
│       │   └── grpc/
│       │       ├── user_handler.go               ← Recibe gRPC, llama al core
│       │       ├── user_mapper.go                ← domain → proto
│       │       └── interceptors.go               ← Recovery, CID, Logging
│       └── outbound/
│           ├── postgres/
│           │   ├── user_repo.go                  ← CRUD de users (embebe SearchEngine)
│           │   ├── permission_repo.go            ← Implementa PermissionRepository
│           │   ├── school_repo.go                ← Implementa SchoolRepository
│           │   ├── search_builder.go             ← Filtros → SQL parametrizado
│           │   ├── search_engine.go              ← Ejecuta SQL + mapea (genérico)
│           │   └── user_schema.go                ← Whitelist de columnas de users
│           ├── redis/
│           │   └── user_cache.go                 ← Implementa UserCache
│           └── bcrypt/
│               └── hasher.go                     ← Implementa PasswordHasher
├── k8s/                                          ← Deployment, Service, ConfigMap
├── buf.yaml + buf.gen.yaml                       ← Config para buf generate
├── go.mod
└── Dockerfile
```

**Regla mental de 5 segundos para saber dónde va algo nuevo:**

| ¿Qué es? | Va en |
|----------|-------|
| Lógica de negocio | `internal/core/` |
| Algo que habla con el exterior (DB, cache, otro servicio) | `internal/adapters/outbound/` |
| Algo que recibe requests | `internal/adapters/inbound/` |
| Helper reutilizable sin infra | `internal/shared/` |
| Contrato con otros servicios | `proto/` |
| Arranque | `cmd/` |

---

## 4. PROTO — el contrato con el mundo

Los archivos `.proto` definen **qué puede hacer** el servicio, qué recibe, qué devuelve. Se compilan a Go con `buf generate`.

### 4.1 `proto/user.proto`

**Qué es:** el contrato principal. Define `UserService` con todos sus RPCs.

```proto
syntax = "proto3";
package users.v1;
option go_package = "users_service/proto/gen;userspb";

import "google/protobuf/timestamp.proto";
import "common/search.proto";
```

**Por qué:**
- `syntax = "proto3"`: versión moderna del formato.
- `package users.v1`: namespace. `v1` en el nombre = si mañana rompes el contrato, lanzas `users.v2` y los clientes no rompen.
- `go_package`: ruta donde irán los `.go` generados.
- `import "common/search.proto"`: reutilizamos tipos compartidos (no duplicar).

```proto
service UserService {
  rpc CreateUser      (CreateUserRequest)     returns (UserResponse);
  rpc GetUser         (GetUserRequest)        returns (UserResponse);
  rpc SearchUsers     (common.v1.SearchRequest) returns (common.v1.SearchResponse);
  // ...
}
```

**Por qué:**
- Cada `rpc` es un método del servicio.
- `SearchUsers` usa tipos de `common.v1.SearchRequest/Response` → **idéntico al formato HubSpot**. Todos los servicios del sistema usarán este mismo contrato para listar.

```proto
message User {
  string id              = 1;  // UUID v7
  string email           = 2;
  string first_name      = 3;
  // ...
}
```

**Por qué los números (`= 1, = 2`):**
- Son **tags binarios**. Identifican el campo en el wire format.
- **Nunca los reutilizas** si eliminas un campo. Añades tags nuevos, dejas los viejos sin uso.

### 4.2 `proto/common/search.proto`

**Qué es:** el contrato de búsqueda compartido entre todos los servicios. Estilo HubSpot: `filterGroups`, `filters`, `operators`, `properties`, `limit`.

```proto
message SearchRequest {
  repeated FilterGroup filter_groups = 1;  // OR entre grupos
  repeated string properties = 2;          // qué devolver
  uint32 limit = 3;
  uint32 after = 4;                        // offset
  repeated SortOrder sorts = 5;
}

message FilterGroup {
  repeated Filter filters = 1;             // AND dentro
}

message Filter {
  string property_name = 1;
  FilterOperator operator = 2;             // EQ, NEQ, IN, CONTAINS, ...
  repeated string values = 3;
}
```

**Por qué esta forma:**

```
      filterGroups = [
        { filters: [f1 AND f2 AND f3] }   ← grupo 1: todo AND
        { filters: [f4 AND f5]       }   ← grupo 2: todo AND
      ]
                    ↑
              OR entre grupos
```

Permite expresar cualquier combinación lógica con solo 2 niveles.

**Ejemplo concreto:**
```json
{
  "filterGroups": [
    { "filters": [
        {"propertyName":"active","operator":"EQ","values":["true"]},
        {"propertyName":"email","operator":"CONTAINS","values":["@gmail"]}
    ]},
    { "filters": [
        {"propertyName":"email","operator":"EQ","values":["admin@x.com"]}
    ]}
  ]
}
```

Traduce a SQL: `WHERE (active = true AND email ILIKE '%@gmail%') OR (email = 'admin@x.com')`.

### 4.3 `proto/common/error.proto`

**Qué es:** envelope de error **igual a HubSpot**. Viaja como `detail` del `google.rpc.Status` gRPC.

```proto
message ErrorResponse {
  string status = 1;                         // siempre "error"
  string message = 2;                        // humano
  repeated ErrorDetail errors = 3;
  ErrorCategory category = 4;
  string correlation_id = 5;                 // para trazar en logs
}

message ErrorDetail {
  string message = 1;
  string code = 2;                           // ej: "EMAIL_TAKEN"
  map<string, StringList> context = 3;       // ej: {"propertyName":["email"]}
}
```

**Por qué `StringList`:**
- Proto3 no permite `map<string, repeated string>`.
- Hay que envolver el array en un mensaje.

**Por qué `correlation_id`:**
- Permite cruzar logs: "esta request que falló en el gateway, qué pasó en users_service?".
- Viaja en el header gRPC `x-correlation-id`.

---

## 5. CORE — la lógica pura

Esta es la capa donde vive la **inteligencia de tu negocio**. Cero imports de `pgx`, `redis`, `grpc`.

### 5.1 `internal/core/domain/user.go`

**Qué es:** la entidad `User` + Value Objects tipados.

```go
package domain

type UserID string
type SchoolID string
type Email string

type User struct {
    ID             UserID
    Email          Email
    PasswordHash   string
    FirstName      string
    LastName       string
    DocumentNumber string
    SchoolID       SchoolID  // "" = sin colegio
    Active         bool
    LastAccessAt   *time.Time
    CreatedAt      time.Time
    UpdatedAt      *time.Time
}
```

**Por qué `UserID string` en vez de `string`:**
- **Value Object tipado.** El compilador te impide confundir un `UserID` con otro `string` cualquiera.
- Antes:
  ```go
  func GetUser(id string) {}    // ¿qué id? ¿user? ¿school? ¿email?
  GetUser("0194f2e7...")        // compila aunque sea un school_id
  ```
- Ahora:
  ```go
  func GetUser(id UserID) {}
  GetUser(SchoolID("..."))     // NO compila
  ```

**Por qué `*time.Time` para `LastAccessAt` y `UpdatedAt`:**
- Pueden ser `NULL` en la DB (usuario nunca accedió, nunca editado).
- `*time.Time` = puede ser `nil`; `time.Time` valor cero es `0001-01-01`, semánticamente ambiguo.

```go
var emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func (e Email) Validate() error {
    s := strings.TrimSpace(string(e))
    if len(s) < 5 || !emailRegex.MatchString(s) {
        return ErrInvalidEmail
    }
    return nil
}

func (e Email) Normalize() Email {
    return Email(strings.ToLower(strings.TrimSpace(string(e))))
}
```

**Por qué métodos en `Email`:**
- Validación vive **al lado del tipo**, no dispersa por 20 lugares.
- Cada vez que veas `Email` en el código, sabes dónde están sus reglas.

### 5.2 `internal/core/domain/school.go`

```go
type School struct {
    ID        SchoolID
    UserID    UserID    // el colegio ES un user
    Name      string
    Active    bool
    CreatedAt time.Time
    UpdatedAt *time.Time
}
```

**Por qué `UserID` y no `string`:**
- Expresa que `school.user_id` apunta a `users.id`. El tipo lo documenta.

### 5.3 `internal/core/domain/permission.go`

```go
type Permission struct {
    ID          uint32
    Scope       string   // "users", "colegios", ...
    Code        string   // "users.create"
    Name        string
    // ...
}

type PermissionGroup struct {
    ID          uint32
    Code        string   // "student_permissions"
    Name        string
    // ...
    Permissions []Permission   // se carga a demanda
}
```

**Por qué `uint32` aquí y no UUID:**
- `permission` es **config interna**: pocos registros, clave natural = `code`.
- UUID añade overhead (joins más lentos, más espacio) sin beneficio real.
- `users` sí usa UUID porque lo consumen otros microservicios que no deben ver IDs secuenciales.

### 5.4 `internal/core/domain/errors.go`

**Qué es:** todos los errores del negocio, tipados.

```go
var (
    ErrUserNotFound   = apperr.NewNotFound("USER_NOT_FOUND", "user not found")
    ErrSchoolNotFound = apperr.NewNotFound("SCHOOL_NOT_FOUND", "school not found")

    ErrEmailTaken    = apperr.NewConflict("EMAIL_TAKEN",
        "email already registered", "email")
    ErrDocumentTaken = apperr.NewConflict("DOCUMENT_TAKEN",
        "document number already registered", "document_number")

    ErrInvalidEmail = apperr.NewValidation("INVALID_EMAIL",
        "email format is invalid", "email")
    ErrWeakPassword = apperr.NewValidation("WEAK_PASSWORD",
        "password must have at least 8 characters", "password")

    ErrInvalidPassword = apperr.NewUnauthenticated(
        "INVALID_CREDENTIALS", "email or password is incorrect")
    ErrUserInactive    = apperr.NewPermissionDenied(
        "USER_INACTIVE", "user account is disabled")
)
```

**Por qué así y no `errors.New("...")`:**
- Cada error YA trae su **Kind** (NotFound/Conflict/Validation/Auth), su **Code** estable, su **Message** humano y el **Field** afectado.
- El handler gRPC NO necesita saber qué es cada error. Solo hace `apperr.ToGRPC(ctx, err)` y el envelope se arma solo.
- Agregar un error nuevo = **una línea aquí**. Nada más se toca.

**Ejemplo visual:** cuando el service devuelve `domain.ErrEmailTaken`, el cliente recibe:
```json
{
  "status": "error",
  "message": "email already registered",
  "errors": [{
    "code": "EMAIL_TAKEN",
    "message": "email already registered",
    "context": {"propertyName": ["email"]}
  }],
  "category": "CONFLICT",
  "correlationId": "a43683b0-..."
}
```

Sin escribir nada en el handler.

### 🧭 Pausa conceptual: inbound vs outbound (modelo mental)

Antes de los dos archivos de `ports/`, una aclaración sobre lo que más confunde del patrón hexagonal: los nombres `inbound` y `outbound` son **relativos al CORE**, no al "yo" del desarrollador.

#### a) Regla direccional — la dirección de las flechas

```
           ┌───────────────────┐
           │                   │
           │       CORE        │
           │                   │
  ───────▶ │  (inbound ports)  │  llamadas ENTRANDO al core
 alguien   │                   │
 afuera    │  (outbound ports) │  ───────▶  llamadas SALIENDO del core
           │                   │           hacia Postgres/Redis/etc.
           └───────────────────┘
```

- **`inbound`** = llamadas que **entran AL core**. Alguien afuera (el handler gRPC, un CLI, un consumer de eventos) invoca al core.
- **`outbound`** = llamadas que **salen DEL core**. El core invoca algo de afuera (la DB, el cache, otro microservicio).

Tu intuición de "outbound = algo que ofrezco al exterior" es al revés. El offer es inbound (las operaciones que ofrezco a que me pidan). Outbound son las **dependencias salientes** que el core necesita para trabajar.

#### b) Nombres alternativos (resuelven la ambigüedad)

Alistair Cockburn, el autor original del patrón, y muchos libros usan términos más claros:

| Estándar | Alternativa | Mnemónica |
|----------|-------------|-----------|
| **inbound** | **driving** | Lo que **conduce** al core (lo dispara). El gRPC handler conduce. |
| **outbound** | **driven** | Lo que el core **conduce** (lo invoca). Postgres es conducido por el core. |

Mentalmente: _¿quién maneja a quién?_
- Los **driving** adapters (inbound) manejan al core.
- El core maneja a los **driven** adapters (outbound).

Los comentarios de `ports/inbound.go` y `ports/outbound.go` usan ambos términos para que puedas asociarlos.

#### c) Regla de decisión en 1 frase

Cuando vayas a crear una interfaz nueva, pregúntate solo una cosa:

| Pregunta | Respuesta → dónde |
|----------|-------------------|
| "¿Alguien afuera puede **pedirle esto** al core?" | **inbound** (`UseCase`) |
| "¿El core **necesita esto de afuera** para trabajar?" | **outbound** (`Repository`, `Cache`, `Hasher`…) |

Ejemplos concretos:

| Necesidad | Tipo | Interfaz |
|-----------|------|----------|
| "Quiero que el gateway pueda crear usuarios" | inbound | `UserUseCase.CreateUser` |
| "Necesito guardar usuarios en Postgres" | outbound | `UserRepository.Save` |
| "Necesito hashear passwords" | outbound | `PasswordHasher.Hash` |
| "Quiero que alguien externo pueda buscar usuarios" | inbound | `UserUseCase.Search` |
| "Necesito cachear usuarios en Redis" | outbound | `UserCache.Set` |
| "Quiero autenticar usuarios" | inbound | `UserUseCase.Authenticate` |
| "La autenticación necesita comparar hashes" | outbound | `PasswordHasher.Compare` |

#### d) Por qué `User` aparece en AMBOS lados

Este es el caso que más confunde. Ves `UserUseCase` en inbound **y** `UserRepository` en outbound y te preguntas por qué la misma entidad en ambos sitios. La respuesta es que **están en direcciones opuestas**:

```
┌──────────────────────────────────────────────────────────────┐
│                                                               │
│   [gRPC handler] ────▶ UserUseCase ─────▶ [UserService]       │
│                       (inbound port)       (core)             │
│                                               │               │
│                                               │               │
│                                               ▼               │
│                                         UserRepository        │
│                                         (outbound port)       │
│                                               │               │
│                                               ▼               │
│                                         [Postgres]            │
│                                                               │
└──────────────────────────────────────────────────────────────┘
```

Comparativa lado a lado:

| | `UserUseCase` (inbound) | `UserRepository` (outbound) |
|---|---|---|
| **Qué responde** | "¿qué puede pedirle el mundo al core sobre usuarios?" | "¿qué necesita el core para persistir/consultar usuarios?" |
| **Dirección** | `mundo → core` | `core → mundo` |
| **Quién la usa (llama sus métodos)** | El handler gRPC | El `UserService` |
| **Quién la implementa** | `UserService` (el core) | `UserRepo` (adapter Postgres) |
| **Métodos típicos** | `CreateUser`, `GetUser`, `Authenticate`, `Search` | `Save`, `FindByID`, `FindByEmail`, `Update` |

Misma entidad (`User`), dos ángulos distintos: lo que **ofreces al mundo** y lo que **necesitas del mundo**.

Lo mismo aplica a `Permission`: `PermissionUseCase` (ofreces: asignar grupo, revocar, listar) vs `PermissionRepository` (necesitas: leer/escribir en `permission_group`, `user_permission_group`).

#### e) Qué son las estructuras `*Cmd` (CreateUserCmd, UpdateUserCmd…)

Son **Command objects** — un patrón de DDD/CQRS. Empaquetan los parámetros de entrada de un caso de uso en un struct.

**Sin Cmd (feo y frágil):**
```go
// firma del método
CreateUser(ctx context.Context, email, password, firstName, lastName,
           docNumber, schoolID string) (*User, error)

// call site
userSvc.CreateUser(ctx, "a@b.c", "pass123", "Ana", "Lopez", "12345", "")
```

**Con Cmd (limpio y evolutivo):**
```go
// firma del método
CreateUser(ctx context.Context, cmd CreateUserCmd) (*User, error)

// call site
userSvc.CreateUser(ctx, ports.CreateUserCmd{
    Email:     "a@b.c",
    Password:  "pass123",
    FirstName: "Ana",
    LastName:  "Lopez",
    DocumentNumber: "12345",
})
```

**Por qué existen los Cmd:**

1. **Evitan firmas gigantes.** Un use case con 8 parámetros es ilegible.
2. **Agregar un campo no rompe callers.** Si agregas `Phone` al `CreateUserCmd`, los callers existentes siguen compilando (el campo queda vacío).
3. **Campos nombrados > argumentos posicionales.** Nunca confundes `firstName` con `lastName`.
4. **Desacoplan la forma del input del transporte.** El handler gRPC arma el `CreateUserCmd` desde un `pb.CreateUserRequest`. Un futuro handler HTTP armaría el mismo `Cmd` desde JSON. El service no se entera de gRPC ni HTTP — solo recibe el `Cmd`.
5. **Pueden tener validación propia.** Si creciera: `func (c CreateUserCmd) Validate() error`.

Los Cmd viven en `ports/inbound.go` porque son **parte del contrato de entrada del core** — no pertenecen al transporte.

> **Convención de nombres**: `XCmd` para comandos de mutación (`CreateUserCmd`, `UpdateUserCmd`, `RevokeAPIKeyCmd`). Para consultas que no mutan puedes usar un struct también (`UserQuery`, `SearchFilter`) pero la convención DDD es llamar `Query` a lo de lectura. En este servicio mantenemos solo `Cmd` por simplicidad.

---

### 5.5 `internal/core/ports/inbound.go`

**Qué es:** qué puede pedirle el mundo al core.

```go
type UserUseCase interface {
    CreateUser(ctx context.Context, cmd CreateUserCmd) (*domain.User, error)
    GetUser(ctx context.Context, id domain.UserID) (*domain.User, error)
    GetUserByEmail(ctx context.Context, email domain.Email) (*domain.User, error)
    UpdateUser(ctx context.Context, cmd UpdateUserCmd) (*domain.User, error)
    DeactivateUser(ctx context.Context, id domain.UserID) error
    Authenticate(ctx, email, password) (*domain.User, []string, error)
    Search(ctx context.Context, req search.Request) (*search.Response, error)
}

type PermissionUseCase interface { /* ... */ }
```

**Por qué interfaces:**
- El handler gRPC recibe `UserUseCase` (interfaz). En producción le pasan `*UserService` (implementación real). En tests le pasan un mock. El handler no sabe ni le importa.
- Eso es **inversión de dependencias** en estado puro.

```go
type CreateUserCmd struct {
    Email          string
    Password       string
    FirstName      string
    LastName       string
    DocumentNumber string
    SchoolID       string
}
```

**Por qué `Cmd` struct en vez de 6 parámetros sueltos:**
- Agregar/quitar campos no rompe firmas de métodos.
- Los adapters (gRPC, HTTP) mapean sus tipos propios a este. Desacopla.

### 5.6 `internal/core/ports/outbound.go`

**Qué es:** qué necesita el core del mundo. Segregado según SOLID (principio I):

```go
type UserReader interface {
    FindByID(ctx, id) (*domain.User, error)
    FindByEmail(ctx, email) (*domain.User, error)
    ExistsByDocument(ctx, doc) (bool, error)
}

type UserWriter interface {
    Save(ctx, u) (UserID, error)
    Update(ctx, u) error
    SetActive(ctx, id, active) error
    TouchLastAccess(ctx, id) error
}

type UserSearcher interface {
    Search(ctx, req) (*search.Response, error)
}

type UserRepository interface {
    UserReader
    UserWriter
    UserSearcher
}
```

**Por qué 3 interfaces + 1 composite:**
- **Cualquier código** (test, futuro servicio read-only) puede depender solo de `UserReader` si solo necesita leer.
- `UserService` usa todo → recibe el composite `UserRepository`.
- Un mismo struct (`UserRepo` de postgres) satisface las 3 porque tiene todos los métodos. Go es duck-typed para interfaces.

```go
type UserCache interface {
    Set(ctx, u) error
    Get(ctx, id) (*User, error)
    Delete(ctx, id) error
}

type PasswordHasher interface {
    Hash(plain string) (string, error)
    Compare(hashed, plain string) error
}
```

**Por qué `PasswordHasher` es interfaz:**
- Si mañana cambias bcrypt por argon2, creas `/argon2/hasher.go` y cambias 1 línea en `main.go`.
- Los tests usan un fake hasher (compara strings) — sin bcrypt real, sin lentitud.

### 5.7 `internal/core/service/user_service.go`

**Qué es:** el **corazón del servicio**. Orquesta dominio + puertos.

```go
type UserService struct {
    users  ports.UserRepository   // interface
    perms  ports.PermissionRepository
    cache  ports.UserCache
    hasher ports.PasswordHasher
}

var _ ports.UserUseCase = (*UserService)(nil)
```

**Por qué `var _ ports.UserUseCase = (*UserService)(nil)`:**
- Garantía de **compilación**. Si `UserService` deja de implementar todos los métodos de `UserUseCase`, el código no compila.
- Es un **test implícito gratis**.

```go
func (s *UserService) CreateUser(ctx context.Context, cmd ports.CreateUserCmd) (*domain.User, error) {
    email := domain.Email(cmd.Email).Normalize()
    if err := email.Validate(); err != nil {
        return nil, err           // ← devuelve ErrInvalidEmail
    }
    if err := domain.ValidatePasswordStrength(cmd.Password); err != nil {
        return nil, err           // ← devuelve ErrWeakPassword
    }

    if existing, err := s.users.FindByEmail(ctx, email); err == nil && existing != nil {
        return nil, domain.ErrEmailTaken
    } else if err != nil && !errors.Is(err, domain.ErrUserNotFound) {
        return nil, err
    }
    // ...
    hash, _ := s.hasher.Hash(cmd.Password)
    u := &domain.User{Email: email, PasswordHash: hash, /* ... */}
    id, _ := s.users.Save(ctx, u)
    u.ID = id

    _ = s.cache.Set(ctx, u)       // ← best-effort, ignora error
    return u, nil
}
```

**Por qué cada línea:**
- `Normalize` → siempre minúsculas y sin espacios. Consistencia.
- `Validate` → email bien formado ANTES de tocar DB.
- `ValidatePasswordStrength` → política de negocio (mínimo 8 chars). Vive en domain, no en el handler.
- `FindByEmail` → detectar duplicado temprano, con mensaje claro (no esperar al error SQL).
- `errors.Is(err, ErrUserNotFound)` → el "no encontrado" es el caso esperado; cualquier OTRO error es un problema real.
- `_ = s.cache.Set(...)` → el `_` dice "sé que puede fallar, no me importa". Si Redis cae, el user se creó igual.

**Ejemplo de lo que NO está aquí (intencional):**
- No hay `db.Query("INSERT INTO users...")`. Eso vive en el adapter.
- No hay `status.Error(codes.AlreadyExists, ...)`. Eso lo hace `apperr.ToGRPC` en la frontera.
- No hay `bcrypt.GenerateFromPassword`. Eso vive en el adapter bcrypt.

```go
func (s *UserService) Authenticate(ctx context.Context, email domain.Email, plain string) (*domain.User, []string, error) {
    email = email.Normalize()
    u, err := s.users.FindByEmail(ctx, email)
    if err != nil {
        return nil, nil, domain.ErrInvalidPassword  // ← NO reveles "email no existe"
    }
    if !u.Active {
        return nil, nil, domain.ErrUserInactive
    }
    if err := s.hasher.Compare(u.PasswordHash, plain); err != nil {
        return nil, nil, domain.ErrInvalidPassword
    }

    codes, err := s.perms.FindCodesByUserID(ctx, u.ID)
    if err != nil { return nil, nil, err }

    _ = s.users.TouchLastAccess(ctx, u.ID)
    _ = s.cache.Delete(ctx, u.ID)
    return u, codes, nil
}
```

**Por qué "NO reveles email no existe":**
- Devolver errores distintos para "email no existe" vs "password incorrecto" permite **enumerar emails** válidos del sistema. Es un ataque real.
- Devolver siempre `ErrInvalidPassword` protege la privacidad.

### 5.8 `internal/core/service/permission_service.go`

```go
type PermissionService struct {
    users ports.UserRepository
    perms ports.PermissionRepository
}

func (s *PermissionService) AssignGroup(ctx, userID, groupID) error {
    if _, err := s.users.FindByID(ctx, userID); err != nil { return err }
    ok, err := s.perms.GroupExists(ctx, groupID)
    if err != nil { return err }
    if !ok { return domain.ErrPermGroupNotFound }
    return s.perms.AssignGroupToUser(ctx, userID, groupID)
}
```

**Por qué chequear existencia antes:**
- El INSERT con FK da un error opaco. Validar primero devuelve `ErrUserNotFound` o `ErrPermGroupNotFound` claros.
- Es "fail fast con buen mensaje".

---

## 6. SHARED — utilitarios transversales

Estos son **conceptos que no son dominio ni infraestructura**: tipos de búsqueda, errores tipados. Los usa tanto el core como los adapters.

### 6.1 `internal/shared/apperr/errors.go`

**Qué es:** el tipo `Error` tipado del sistema entero.

```go
type Kind int

const (
    KindUnspecified Kind = iota
    KindValidation
    KindConflict
    KindNotFound
    KindUnauthenticated
    KindPermissionDenied
    KindInternal
)

type Error struct {
    Kind    Kind
    Code    string   // "EMAIL_TAKEN"
    Message string   // "email already registered"
    Field   string   // "email" (opcional)
    cause   error    // original, solo para logs
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.cause }
```

**Por qué:**
- Un único tipo para TODOS los errores del sistema.
- `Kind` = taxonomía universal (no atada a gRPC). Cualquier transporte mapea después.
- `Code` = machine-friendly ("EMAIL_TAKEN"). El cliente lo usa en switches.
- `Message` = humano para mostrar.
- `Field` = qué campo falló (para validation errors).
- `cause` minúscula = privado, solo para `errors.Unwrap()` (logging). Nunca se expone al cliente.

```go
func NewConflict(code, message, field string) *Error {
    return &Error{Kind: KindConflict, Code: code, Message: message, Field: field}
}
// ... NewValidation, NewNotFound, NewUnauthenticated, ...
```

**Por qué constructores:**
- Firma simple; no usas struct literal con 5 campos.
- `NewNotFound("USER_NOT_FOUND", "user not found")` — dos strings, ya está.

```go
type ctxKey struct{}
var correlationIDKey = ctxKey{}

func WithCorrelationID(ctx, id) context.Context {
    return context.WithValue(ctx, correlationIDKey, id)
}

func CorrelationIDFromContext(ctx) string {
    if v, ok := ctx.Value(correlationIDKey).(string); ok { return v }
    return ""
}
```

**Por qué `type ctxKey struct{}`:**
- Es el idioma Go para "clave única de context". Dos packages no pueden chocar accidentalmente.
- Si usaras `"correlation-id"` como clave, otro paquete con la misma string sobrescribiría.

### 6.2 `internal/shared/apperr/apperr.go`

**Qué es:** el traductor único de errores a gRPC.

```go
func ToGRPC(ctx context.Context, err error) error {
    if err == nil { return nil }

    var ae *Error
    if !errors.As(err, &ae) {
        ae = NewInternal("INTERNAL_ERROR",
            "an unexpected error occurred", err)
    }

    code, category := transportFor(ae.Kind)

    detail := &commonpb.ErrorDetail{Code: ae.Code, Message: ae.Message}
    if ae.Field != "" {
        detail.Context = map[string]*commonpb.StringList{
            "propertyName": {Values: []string{ae.Field}},
        }
    }

    payload := &commonpb.ErrorResponse{
        Status:        "error",
        Message:       ae.Message,
        Errors:        []*commonpb.ErrorDetail{detail},
        Category:      category,
        CorrelationId: CorrelationIDFromContext(ctx),
    }

    st := status.New(code, ae.Message)
    if withDetails, derr := st.WithDetails(payload); derr == nil {
        return withDetails.Err()
    }
    return st.Err()
}
```

**Flujo visual de un error:**

```
Service devuelve            ─▶  handler hace         ─▶ Cliente recibe
domain.ErrEmailTaken            apperr.ToGRPC(ctx,e)    JSON envelope
│                               │                       │
│ Kind: Conflict                │ codes.AlreadyExists   │ "category": "CONFLICT"
│ Code: "EMAIL_TAKEN"           │ payload attached      │ "code": "EMAIL_TAKEN"
│ Message: "email already..."   │                       │ "message": "..."
│ Field: "email"                │                       │ "context": {"propertyName":["email"]}
└────────────────────────────   └──────────────────     └────────────────
```

**Por qué `errors.As`:**
- Navega la cadena de `Unwrap()`. Encuentra el `*apperr.Error` aunque esté wrapeado con `fmt.Errorf("%w: ctx", err)`.

**Por qué el fallback a `NewInternal`:**
- Si `err` no es nuestro tipo (bug, panic no capturado, error inesperado de pgx), devolvemos "internal error" genérico SIN exponer detalles internos.
- El error original queda en `cause` para logs.

```go
func transportFor(k Kind) (codes.Code, commonpb.ErrorCategory) {
    switch k {
    case KindValidation:       return codes.InvalidArgument,  VALIDATION_ERROR
    case KindConflict:         return codes.AlreadyExists,    CONFLICT
    case KindNotFound:         return codes.NotFound,         NOT_FOUND
    // ...
    }
}
```

**Por qué el único switch del sistema:**
- **Un switch de 6 casos** (uno por Kind) reemplaza los 15 switches que antes vivían en el mapper.
- Si agregas una categoría nueva (ej: `KindRateLimit`), añades un caso aquí y listo.

### 6.3 `internal/shared/search/types.go`

**Qué es:** los tipos Go puros del motor de búsqueda (sin proto ni SQL).

```go
type Request struct {
    FilterGroups []FilterGroup
    Properties   []string
    Limit        int
    After        int
    Sorts        []Sort
}

type FilterGroup struct {
    Filters []Filter
}

type Filter struct {
    Property string
    Operator Operator
    Values   []string
}

type Operator string

const (
    OpEQ          Operator = "EQ"
    OpNEQ         Operator = "NEQ"
    OpLT          Operator = "LT"
    // ...
    OpBetween     Operator = "BETWEEN"
)
```

**Por qué tipos propios (no usar proto directo):**
- El core no debería conocer `commonpb.SearchRequest`. Trabaja con tipos Go limpios.
- La conversión se hace UNA vez (en `proto.go`).
- Si mañana cambias de gRPC a REST, solo cambia la conversión.

```go
type Response struct {
    Total   uint32
    Results []Result
    Paging  *Paging
}

type Result struct {
    ID         string
    CreatedAt  time.Time
    UpdatedAt  time.Time
    Archived   bool
    Properties map[string]any
}
```

**Por qué `Properties map[string]any`:**
- Cada entidad tiene columnas distintas (users tiene `email`, exams tendrá `score`, etc.).
- Un map dinámico permite que el mismo tipo sirva para todos.

### 6.4 `internal/shared/search/errors.go`

```go
func UnknownProperty(name string) *apperr.Error {
    return apperr.NewValidation("UNKNOWN_PROPERTY",
        fmt.Sprintf("unknown property: %s", name), name)
}

func PropertyNotFilterable(name string) *apperr.Error {
    return apperr.NewValidation("PROPERTY_NOT_FILTERABLE",
        fmt.Sprintf("property not filterable: %s", name), name)
}
// ...
```

**Por qué factories en vez de sentinels:**
- El error lleva el contexto REAL: "unknown property: **email_typo**".
- El cliente ve qué propiedad rompió, sin parsing de mensajes.

### 6.5 `internal/shared/search/proto.go`

**Qué es:** el adaptador pb ↔ Go (único lugar que importa `commonpb`).

```go
func RequestFromProto(p *commonpb.SearchRequest) Request {
    r := Request{
        Properties: p.GetProperties(),
        Limit:      int(p.GetLimit()),
        After:      int(p.GetAfter()),
    }
    for _, g := range p.GetFilterGroups() {
        group := FilterGroup{}
        for _, f := range g.GetFilters() {
            group.Filters = append(group.Filters, Filter{
                Property: f.GetPropertyName(),
                Operator: operatorFromPB(f.GetOperator()),
                Values:   f.GetValues(),
            })
        }
        r.FilterGroups = append(r.FilterGroups, group)
    }
    return r
}
```

**Por qué `p.GetX()` y no `p.X`:**
- `GetX()` es nil-safe: si `p` es nil, devuelve el valor cero sin panic.
- Idioma estándar de proto3 en Go.

```go
func ResponseToProto(r *Response) (*commonpb.SearchResponse, error) {
    // ...
    for _, res := range r.Results {
        props, err := structpb.NewStruct(toStructValues(res.Properties))
        // ...
    }
}

func toStructScalar(v any) any {
    switch x := v.(type) {
    case time.Time:
        return x.Format(time.RFC3339Nano)
    case int64:
        return float64(x)     // structpb no acepta int64
    // ...
    }
}
```

**Por qué `structpb.NewStruct`:**
- `Properties` es un map dinámico. gRPC lo transporta con `google.protobuf.Struct`.
- `structpb` solo acepta ciertos tipos (string, bool, float64, nil, list, map). Todo lo demás (time, int64) se stringea o se convierte.

---

## 7. INBOUND — cómo entran las requests

### 7.1 `internal/adapters/inbound/grpc/user_handler.go`

**Qué es:** el handler gRPC. **Solo traduce** — nunca tiene lógica de negocio.

```go
type UserHandler struct {
    pb.UnimplementedUserServiceServer      // ← embedding
    users ports.UserUseCase                 // ← interface
    perms ports.PermissionUseCase
}
```

**Por qué `UnimplementedUserServiceServer`:**
- Lo genera `buf`. Te da implementaciones default (que devuelven `Unimplemented`) de todos los métodos del servicio.
- Si agregas un RPC nuevo al `.proto`, el servicio sigue compilando hasta que estés listo para implementarlo.

```go
func (h *UserHandler) CreateUser(ctx, req *pb.CreateUserRequest) (*pb.UserResponse, error) {
    u, err := h.users.CreateUser(ctx, ports.CreateUserCmd{
        Email:          req.GetEmail(),
        Password:       req.GetPassword(),
        FirstName:      req.GetFirstName(),
        // ...
    })
    if err != nil {
        return nil, apperr.ToGRPC(ctx, err)
    }
    return &pb.UserResponse{User: toProtoUser(u)}, nil
}
```

**Por qué este handler es "tonto":**
- Mapea proto → cmd, llama al core, mapea user → proto.
- **Una línea** para el error: `apperr.ToGRPC(ctx, err)`. Todo el envelope se arma solo.
- Si la lógica cambia, el handler NO se toca.

```go
func (h *UserHandler) SearchUsers(ctx, req *commonpb.SearchRequest) (*commonpb.SearchResponse, error) {
    resp, err := h.users.Search(ctx, search.RequestFromProto(req))
    if err != nil {
        return nil, apperr.ToGRPC(ctx, err)
    }
    return search.ResponseToProto(resp)
}
```

**Por qué solo 3 líneas útiles:**
- Todo el trabajo está delegado. Conversión en `search.RequestFromProto`, lógica en `h.users.Search`, conversión de vuelta en `ResponseToProto`.

### 7.2 `internal/adapters/inbound/grpc/user_mapper.go`

**Qué es:** conversión entity → proto.

```go
func toProtoUser(u *domain.User) *pb.User {
    if u == nil { return nil }
    return &pb.User{
        Id:             string(u.ID),
        Email:          string(u.Email),
        FirstName:      u.FirstName,
        // ...
        LastAccessAt:   tsOrNil(u.LastAccessAt),
        CreatedAt:      timestamppb.New(u.CreatedAt),
        UpdatedAt:      tsOrNil(u.UpdatedAt),
    }
}

func tsOrNil(t *time.Time) *timestamppb.Timestamp {
    if t == nil { return nil }
    return timestamppb.New(*t)
}
```

**Por qué separado del handler:**
- SRP: un archivo por razón de cambio. Si cambia la forma del `pb.User`, cambia solo aquí.
- Reutilizable: si un día tienes HTTP además de gRPC, puedes compartir.

### 7.3 `internal/adapters/inbound/grpc/interceptors.go`

**Qué es:** middleware gRPC. Se ejecuta alrededor de cada RPC.

```go
const metadataHeaderCorrelationID = "x-correlation-id"

func CorrelationIDInterceptor(ctx, req, info, handler) (any, error) {
    id := ""
    if md, ok := metadata.FromIncomingContext(ctx); ok {
        if v := md.Get(metadataHeaderCorrelationID); len(v) > 0 && v[0] != "" {
            id = v[0]                         // ← reusa el que mandó el caller
        }
    }
    if id == "" {
        id = uuid.NewString()                 // ← genera uno nuevo
    }
    ctx = apperr.WithCorrelationID(ctx, id)
    _ = grpc.SetHeader(ctx, metadata.Pairs(metadataHeaderCorrelationID, id))
    return handler(ctx, req)
}
```

**Por qué reusar el ID si viene:**
- Trazabilidad end-to-end. Gateway genera ID → users_service lo reutiliza → si users_service llama a otro servicio, lo reutiliza también.
- Logs de 3 servicios se pueden cruzar por el mismo `cid`.

```go
func LoggingInterceptor(ctx, req, info, handler) (any, error) {
    start := time.Now()
    resp, err := handler(ctx, req)
    log.Printf("grpc method=%s code=%s dur=%s cid=%s",
        info.FullMethod, status.Code(err), time.Since(start),
        apperr.CorrelationIDFromContext(ctx),
    )
    return resp, err
}
```

**Por qué log al final:**
- Necesitas saber duración + código del error. Antes del handler no sabes ni lo uno ni lo otro.

```go
func RecoveryInterceptor(ctx, req, info, handler) (resp any, err error) {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("panic in %s: %v\n%s", info.FullMethod, r, debug.Stack())
            err = status.Error(codes.Internal, "internal error")
        }
    }()
    return handler(ctx, req)
}
```

**Por qué existe:**
- Un `panic` en Go mata el proceso entero. Con esto, un panic en el handler se convierte en un error 500 y el servicio sigue vivo.
- Stack trace en el log para debug.

**Cómo se encadenan (en main.go):**
```go
grpc.ChainUnaryInterceptor(
    RecoveryInterceptor,       // 1. outermost: captura panics de TODO
    CorrelationIDInterceptor,  // 2. inyecta cid antes del log
    LoggingInterceptor,        // 3. ve cid, logea
)
```

Flujo visual:
```
request ─▶ Recovery ─▶ CID ─▶ Logging ─▶ Handler
                                            │
                                            ▼
                                       UserService
                                            │
                                            ▼
                                       UserRepo
                                            │
response ◀─ Recovery ◀─ CID ◀─ Logging ◀───┘
               ↑
          captura panics
```

---

## 8. OUTBOUND — cómo hablo con el mundo

### 8.0 Antes de los repos: ¿qué es `*pgxpool.Pool`?

Todos los repos de Postgres reciben un `*pgxpool.Pool`. Es la **manija única** para hablar con la DB.

**Qué es:** un **grupo de conexiones a Postgres reutilizables**, gestionado automáticamente.

**Por qué existe (problema sin pool):**

```
Sin pool:
  Req 1 ─▶ abrir conexión (50ms) ─▶ query ─▶ cerrar conexión
  Req 2 ─▶ abrir conexión (50ms) ─▶ query ─▶ cerrar conexión
  Req 3 ─▶ abrir conexión (50ms) ─▶ query ─▶ cerrar conexión
```

Abrir una conexión TCP + autenticación a Postgres cuesta ~50ms. Con 1000 req/s, colapsa todo.

**Con pool:**

```
┌──────────────────────────────────────────────┐
│                    POOL                       │
│   ┌───┐  ┌───┐  ┌───┐  ┌───┐  ┌───┐         │   ← N conexiones
│   │ 1 │  │ 2 │  │ 3 │  │ 4 │  │ 5 │ ...     │      ya autenticadas
│   └───┘  └───┘  └───┘  └───┘  └───┘         │
└──────────────────────────────────────────────┘
     ▲       ▲       ▲
     │       │       │
 Req 1   Req 2   Req 3 ...           ← piden una libre, la usan, la devuelven
```

Las conexiones **nunca se cierran**. Se marcan "en uso" o "libre".

**Lo que configura `main.go`:**

```go
cfg.MaxConns        = 25              // máximo 25 conexiones simultáneas
cfg.MinConns        = 2               // siempre al menos 2 "tibias"
cfg.MaxConnLifetime = 5 * time.Minute // recicla conexiones viejas
```

| Parámetro | Para qué |
|-----------|----------|
| `MaxConns` | Tope. Evita saturar a Postgres. |
| `MinConns` | Mínimo siempre caliente. Evita latencia al primer request. |
| `MaxConnLifetime` | Recicla periódicamente. Evita fugas y problemas de red/balanceador. |

**Métodos principales que verás:**

```go
pool.Query(ctx, sql, args...)      // SELECT con varias filas
pool.QueryRow(ctx, sql, args...)   // SELECT con una fila
pool.Exec(ctx, sql, args...)       // INSERT/UPDATE/DELETE
pool.Begin(ctx)                    // inicia transacción
pool.Ping(ctx)                     // healthcheck
```

**Todos los repos comparten el mismo pool** (creado una sola vez en main.go). No quieres uno por repo — eso multiplicaría las conexiones.

**Thread-safe:** varias goroutines pueden llamarlo a la vez, el pool sincroniza internamente.

---

### 8.1 `internal/adapters/outbound/postgres/user_repo.go`

**Qué es:** el archivo que habla SQL de `users` (CRUD). La búsqueda la hereda por embedding.

```go
// Embebe *SearchEngine => UserRepo gana Search() automáticamente por
// promoción de métodos. La única pieza específica de users es el
// schema (ver user_schema.go).
type UserRepo struct {
    pool *pgxpool.Pool
    *SearchEngine
}

var _ ports.UserRepository = (*UserRepo)(nil)

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
    return &UserRepo{
        pool:         pool,
        SearchEngine: NewSearchEngine(pool, userSearchSchema),
    }
}
```

**Por qué embedding `*SearchEngine`:**
- Go promociona los métodos del campo embebido al struct contenedor.
- `UserRepo.Search(ctx, req)` existe aunque no lo escribas — es el del engine.
- Cualquier otro repo (School, Exam, Order...) con la misma técnica gana búsqueda **gratis**.

**Por qué `*pgxpool.Pool` y no `*sql.DB`:**
- `pgx` nativo es más rápido (menos alocaciones) que `database/sql`.
- API más expresiva (`QueryRow`, `CollectRows`, types nativos para UUID/timestamp).

```go
const userCols = `id::text,
    email,
    password_hash,
    COALESCE(first_name, ''),
    COALESCE(last_name, ''),
    COALESCE(document_number, ''),
    COALESCE(school_id::text, ''),
    active,
    last_access_at,
    created_at,
    updated_at`
```

**Por qué `COALESCE(x, '')` al leer:**
- Columnas NULLables. Devolver "" en vez de NULL es más simple para mapear a string.
- `school_id::text` → convierte UUID a string antes de leer.

```go
func (r *UserRepo) Save(ctx, u *domain.User) (domain.UserID, error) {
    const q = `
        INSERT INTO users (email, password_hash, first_name, last_name, document_number, school_id, active)
        VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, '')::uuid, $7)
        RETURNING id::text`

    var id string
    err := r.pool.QueryRow(ctx, q, ...).Scan(&id)
    if err != nil { return "", mapDuplicate(err) }
    return domain.UserID(id), nil
}
```

**Por qué `NULLIF($3, '')`:**
- `first_name` es NULL-able. Si el user manda "", queremos NULL en DB.
- `NULLIF(x, '')` = `CASE WHEN x = '' THEN NULL ELSE x END`.

**Por qué `RETURNING id::text`:**
- Postgres genera el UUID con `DEFAULT uuidv7()`. Necesitamos recuperarlo.
- `::text` para que pgx lo devuelva como string limpio (sin UUID binario).

```go
func mapDuplicate(err error) error {
    var pgErr *pgconn.PgError
    if errors.As(err, &pgErr) && pgErr.Code == "23505" {
        switch pgErr.ConstraintName {
        case "uk_user_email":           return domain.ErrEmailTaken
        case "uk_user_document_number": return domain.ErrDocumentTaken
        }
    }
    return err
}
```

**Por qué aquí:**
- `23505` = `unique_violation` en Postgres.
- **Este es el único lugar del código** que conoce códigos SQLSTATE. El resto del sistema solo ve `domain.ErrEmailTaken`.

### 8.2 `internal/adapters/outbound/postgres/permission_repo.go`

```go
func (r *PermissionRepo) FindCodesByUserID(ctx, userID domain.UserID) ([]string, error) {
    const q = `
        SELECT DISTINCT p.code
          FROM user_permission_group upg
          JOIN permission_group pg        ON pg.id = upg.permission_group_id AND pg.active = TRUE
          JOIN permission_group_permission pgp ON pgp.permission_group_id = pg.id
          JOIN permission p               ON p.id = pgp.permission_id AND p.active = TRUE
         WHERE upg.user_id = $1::uuid`
    // ... scan rows ...
}
```

**Por qué este JOIN:**

Visual:
```
 users (id)
    │ fk
    ▼
 user_permission_group (user_id, permission_group_id)
    │ fk
    ▼
 permission_group (id, code, active)
    │ fk (via pgp)
    ▼
 permission_group_permission (group_id, permission_id)
    │ fk
    ▼
 permission (id, code, active)
```

Un usuario → N grupos → N permisos cada uno. El JOIN recorre toda la cadena. `DISTINCT` porque dos grupos pueden traer el mismo permiso.

```go
func (r *PermissionRepo) AssignGroupToUser(ctx, userID, groupID) error {
    const q = `
        INSERT INTO user_permission_group (user_id, permission_group_id)
        VALUES ($1::uuid, $2)
        ON CONFLICT (user_id, permission_group_id) DO NOTHING`
    _, err := r.pool.Exec(ctx, q, string(userID), groupID)
    return err
}
```

**Por qué `ON CONFLICT DO NOTHING`:**
- Asignar dos veces el mismo grupo no es error. Idempotente.

### 8.3 `internal/adapters/outbound/postgres/school_repo.go`

Similar a user_repo pero para `school`. Un método: `FindByID`. Pequeño y cohesivo.

### 8.4 `internal/adapters/outbound/postgres/search_builder.go`

**Qué es:** motor genérico para convertir `search.Request` → SQL parametrizado. **Funciona para CUALQUIER tabla** que tenga un schema.

```go
type SearchSchema struct {
    Table        string
    IDColumn     string
    CreatedCol   string
    UpdatedCol   string
    ArchivedExpr string
    Columns      map[string]SearchColumn
    DefaultLimit int
    MaxLimit     int
    DefaultSort  []search.Sort
}

type SearchColumn struct {
    DBName     string        // columna real en la tabla
    Type       SearchColType // para cast correcto
    Filterable bool
    Sortable   bool
    Selectable bool
}
```

**Por qué:**
- **Whitelist absoluta.** Solo las columnas declaradas aquí son visibles al mundo. `password_hash` no está → imposible exponerlo.
- Cada columna dice si se puede filtrar, ordenar, devolver. Granular.

```go
func BuildSearch(s SearchSchema, req search.Request) (*BuiltQuery, error) {
    // ... selecciona propiedades validas ...
    cols := []string{
        fmt.Sprintf("%s::text AS __id", s.IDColumn),
        fmt.Sprintf("%s AS __created_at", s.CreatedCol),
        fmt.Sprintf("%s AS __updated_at", s.UpdatedCol),
    }
    cols = append(cols, "COUNT(*) OVER() AS __total")  // ← total en la misma query
    // ...
}
```

**Por qué `COUNT(*) OVER()`:**
- Es un **window function**. Cuenta TODAS las filas que cumplen WHERE (antes del LIMIT) y lo pega en cada fila.
- **Una sola query** devuelve resultados + total. Cero roundtrips extra.

```
SELECT id::text AS __id,
       created_at AS __created_at,
       ...
       COUNT(*) OVER() AS __total,
       email AS "email",
       first_name AS "first_name"
  FROM users
 WHERE (active = $1::boolean AND email ILIKE $2)
    OR (email = $3)
 ORDER BY created_at DESC, id DESC
 LIMIT $4 OFFSET $5
```

**Por qué `$N` (placeholders) en vez de concatenar strings:**
- **Protege contra SQL injection.** Los valores nunca tocan el SQL como texto; van por canal separado.
- Si alguien manda `'; DROP TABLE users;--` como value, Postgres lo trata como string literal.

```go
func buildFilter(col SearchColumn, f search.Filter, args *[]any) (string, error) {
    cast := castExpr(col.Type)
    ph := func(v string) string {
        *args = append(*args, v)
        return fmt.Sprintf("$%d%s", len(*args), cast)
    }

    switch f.Operator {
    case search.OpEQ:
        return fmt.Sprintf("%s = %s", col.DBName, ph(f.Values[0])), nil
    case search.OpIN:
        parts := make([]string, len(f.Values))
        for i, v := range f.Values { parts[i] = ph(v) }
        return fmt.Sprintf("%s IN (%s)", col.DBName, strings.Join(parts, ", ")), nil
    case search.OpContains:
        *args = append(*args, "%"+f.Values[0]+"%")
        return fmt.Sprintf("%s ILIKE $%d", col.DBName, len(*args)), nil
    // ...
    }
}
```

**Por qué `castExpr`:**
- Los valores llegan como `string` en el proto. Pero la columna puede ser UUID, timestamp, int.
- `$N::uuid` le dice a Postgres "este string es un UUID".

Tabla de casts:
| Tipo Go | Cast SQL |
|---------|----------|
| TypeString | (ninguno) |
| TypeInt | ::bigint |
| TypeUUID | ::uuid |
| TypeBool | ::boolean |
| TypeTimestamp | ::timestamptz |

### 8.5 `internal/adapters/outbound/postgres/search_engine.go`

**Qué es:** ejecutor genérico. Corre la query + mapea resultados. **Mismo código para cualquier tabla.**

```go
type SearchEngine struct {
    pool   *pgxpool.Pool
    schema SearchSchema
}

func NewSearchEngine(pool *pgxpool.Pool, schema SearchSchema) *SearchEngine {
    return &SearchEngine{pool: pool, schema: schema}
}

func (e *SearchEngine) Search(ctx context.Context, req search.Request) (*search.Response, error) {
    q, err := BuildSearch(e.schema, req)           // 1. construir SQL
    if err != nil { return nil, err }

    rows, err := e.pool.Query(ctx, q.SQL, q.Args...) // 2. ejecutar
    if err != nil { return nil, err }
    defer rows.Close()

    maps, err := pgx.CollectRows(rows, pgx.RowToMap) // 3. rows → []map
    if err != nil { return nil, err }

    // 4. separar metadata (__id, __total, etc.) de properties reales
    resp := &search.Response{Results: make([]search.Result, 0, len(maps))}
    for _, m := range maps {
        if v, ok := m["__total"].(int64); ok { resp.Total = uint32(v) }

        id, _       := m["__id"].(string)
        archived, _ := m["__archived"].(bool)
        createdAt, _:= m["__created_at"].(time.Time)
        updatedAt, _:= m["__updated_at"].(time.Time)

        delete(m, "__id"); delete(m, "__created_at"); delete(m, "__updated_at")
        delete(m, "__archived"); delete(m, "__total")

        resp.Results = append(resp.Results, search.Result{
            ID: id, CreatedAt: createdAt, UpdatedAt: updatedAt,
            Archived: archived, Properties: m,
        })
    }
    // 5. paginación
    resp.Paging = &search.Paging{
        HasMore:   uint32(req.After)+uint32(len(resp.Results)) < resp.Total,
        NextAfter: uint32(req.After) + uint32(len(resp.Results)),
    }
    return resp, nil
}
```

**Por qué `pgx.RowToMap`:**
- Devuelve `map[string]any` con el nombre de columna como clave.
- Perfecto para `Properties` dinámico: cualquier columna que selecciones, termina en el map.

**Por qué `SearchEngine` es un struct aparte (en vez de método suelto):**
- Encapsula `pool + schema` juntos. Al embeberlo en un repo, el repo no necesita ver la pareja separada.
- Es **composable**: cualquier repo que lo embeba gana `Search()` vía promoción:

```
┌─────────────────────────┐
│    *SearchEngine        │    ← definido una sola vez
│                         │
│    Search(ctx, req)     │
└───────────┬─────────────┘
            │ embed
   ┌────────┼────────┬────────┐
   ▼        ▼        ▼        ▼
 UserRepo  SchoolRepo  ExamRepo  OrderRepo
 +userSch  +schoolSch  +examSch  +orderSch
 .Search() .Search()   .Search() .Search()
   ↑         ↑          ↑         ↑
  promocion de metodo — mismo codigo corriendo
```

### 8.6 `internal/adapters/outbound/postgres/user_schema.go`

**Qué es:** la única pieza específica de users para búsqueda. Whitelist de columnas.

```go
var userSearchSchema = SearchSchema{
    Table:        "users",
    IDColumn:     "id",
    CreatedCol:   "created_at",
    UpdatedCol:   "updated_at",
    ArchivedExpr: "NOT active",
    DefaultLimit: 50,
    MaxLimit:     200,
    Columns: map[string]SearchColumn{
        "email":       {DBName: "email",      Type: SearchTypeString,    Filterable: true, Sortable: true, Selectable: true},
        "first_name":  {DBName: "first_name", Type: SearchTypeString,    Filterable: true, Sortable: true, Selectable: true},
        "last_name":   {DBName: "last_name",  Type: SearchTypeString,    Filterable: true, Sortable: true, Selectable: true},
        "school_id":   {DBName: "school_id",  Type: SearchTypeUUID,      Filterable: true, Sortable: false, Selectable: true},
        "active":      {DBName: "active",     Type: SearchTypeBool,      Filterable: true, Sortable: false, Selectable: true},
        "created_at":  {DBName: "created_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true, Selectable: true},
        // ... NOTA: password_hash NO está aquí ...
    },
}
```

**Por qué `password_hash` NO está:**
- Omitir del schema = **imposible** devolverlo en properties, imposible filtrar por él, imposible ordenar por él.
- La whitelist es la fuente de verdad.

**Por qué este archivo es pequeño:**
- El trabajo pesado (construir SQL, ejecutar, mapear) vive en `search_builder.go` + `search_engine.go`.
- Aquí solo **declaras qué expones**. Añadir un campo filtrable = una línea.

### 8.7 `internal/adapters/outbound/redis/user_cache.go`

```go
type UserCache struct {
    client *redis.Client
    ttl    time.Duration
}

func (c *UserCache) Set(ctx, u *domain.User) error {
    data, err := json.Marshal(u)
    if err != nil { return err }
    return c.client.Set(ctx, userKey(u.ID), data, c.ttl).Err()
}

func (c *UserCache) Get(ctx, id UserID) (*User, error) {
    data, err := c.client.Get(ctx, userKey(id)).Bytes()
    if errors.Is(err, redis.Nil) {
        return nil, domain.ErrUserNotFound   // ← cache miss = "no encontrado"
    }
    // ...
}

func userKey(id UserID) string {
    return fmt.Sprintf("users_service:user:%s", id)
}
```

**Por qué `users_service:user:<id>`:**
- Namespace del servicio al inicio. Evita colisiones si Redis es compartido.
- Formato consistente = fácil de buscar con `SCAN users_service:*`.

**Patrón cache-aside visual:**
```
GetUser(id)
    │
    ▼
┌──────────┐  HIT   ┌────────────┐
│ Redis    │───────▶│ devuelve   │
│ GET key  │        └────────────┘
└────┬─────┘
     │ MISS
     ▼
┌──────────┐
│ Postgres │
│ SELECT   │
└────┬─────┘
     │
     ▼
┌──────────┐
│ Redis    │
│ SET key  │
│ (15 min) │
└────┬─────┘
     │
     ▼
 devuelve
```

### 8.8 `internal/adapters/outbound/bcrypt/hasher.go`

```go
type Hasher struct{ cost int }

func New(cost int) *Hasher {
    if cost < bcrypt.MinCost { cost = bcrypt.DefaultCost }
    return &Hasher{cost: cost}
}

func (h *Hasher) Hash(plain string) (string, error) {
    b, err := bcrypt.GenerateFromPassword([]byte(plain), h.cost)
    return string(b), err
}

func (h *Hasher) Compare(hashed, plain string) error {
    return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
}
```

**Por qué `cost`:**
- bcrypt es lento adrede. Más cost = más lento = más seguro contra brute force.
- 10 = default (≈100ms). 12 = más seguro pero 400ms. Config tunable.

---

## 9. COMPOSITION — cómo se arma todo

### 9.1 `config/config.go`

```go
type Config struct {
    GRPCPort      string
    DatabaseDSN   string
    RedisAddr     string
    CacheTTL      time.Duration
    BcryptCost    int
    ShutdownWait  time.Duration
    // ...
}

func Load() *Config {
    return &Config{
        GRPCPort:    getEnv("GRPC_PORT", ":50051"),
        DatabaseDSN: getEnv("DATABASE_DSN", "postgres://..."),
        CacheTTL:    getEnvDuration("CACHE_TTL", 15*time.Minute),
        // ...
    }
}
```

**Por qué:**
- Todas las ENV vars en un solo lugar tipado.
- Defaults sensatos para dev local.
- En K8s, las inyecta el ConfigMap/Secret.

### 9.2 `cmd/main.go` — el **Composition Root**

Este es el **único** archivo donde las piezas concretas se conectan.

```go
func main() {
    cfg := config.Load()

    // ---------- 1. INFRAESTRUCTURA ----------
    pool, _ := newPgPool(cfg.DatabaseDSN)        // Postgres
    rdb := redis.NewClient(&redis.Options{...})  // Redis

    // ---------- 2. ADAPTERS OUTBOUND ----------
    userRepo := postgresadapter.NewUserRepo(pool)
    permRepo := postgresadapter.NewPermissionRepo(pool)
    cache    := redisadapter.NewUserCache(rdb, cfg.CacheTTL)
    hasher   := bcryptadapter.New(cfg.BcryptCost)

    // ---------- 3. CORE ----------
    userSvc := service.NewUserService(userRepo, permRepo, cache, hasher)
    permSvc := service.NewPermissionService(userRepo, permRepo)

    // ---------- 4. ADAPTER INBOUND ----------
    handler := grpchandler.NewUserHandler(userSvc, permSvc)

    // ---------- 5. SERVER ----------
    s := grpc.NewServer(
        grpc.ChainUnaryInterceptor(
            grpchandler.RecoveryInterceptor,
            grpchandler.CorrelationIDInterceptor,
            grpchandler.LoggingInterceptor,
        ),
    )
    pb.RegisterUserServiceServer(s, handler)

    // ... health + graceful shutdown ...
}
```

**Por qué este orden:**
1. Config primero (todo lo demás depende de él).
2. Infra (pool, redis client).
3. Outbound adapters (implementan ports, reciben infra).
4. Core services (reciben ports, no saben nada de pool/redis).
5. Inbound adapter (recibe core).
6. Server (registra handler).

**Si mañana cambias Postgres por MongoDB:**
- Cambias `postgresadapter.NewUserRepo(pool)` por `mongoadapter.NewUserRepo(client)`.
- Nada más se toca. Ni el core, ni el handler, ni el proto.

```go
func newPgPool(dsn string) (*pgxpool.Pool, error) {
    cfg, _ := pgxpool.ParseConfig(dsn)
    cfg.MaxConns = 25
    cfg.MinConns = 2
    cfg.MaxConnLifetime = 5 * time.Minute

    pool, _ := pgxpool.NewWithConfig(context.Background(), cfg)

    // Ping con reintentos (en K8s Postgres puede arrancar después).
    for i := 0; i < 10; i++ {
        if err := pool.Ping(ctx); err == nil { return pool, nil }
        time.Sleep(time.Second)
    }
    // ...
}
```

**Por qué ping con reintentos:**
- En Kubernetes, el pod de users_service puede arrancar antes que Postgres esté ready. Reintentos evitan crashear.

```go
sig := make(chan os.Signal, 1)
signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
<-sig

s.GracefulStop()    // ← espera a que terminen las requests en vuelo
```

**Por qué graceful shutdown:**
- Cuando K8s manda SIGTERM (rolling deploy, autoscaling), tenemos 10s para terminar requests en curso.
- Sin esto, requests en medio del proceso fallan con "connection reset".

---

## 10. Ejemplos visuales completos

### 10.1 CreateUser — flujo completo con cada línea

**Request gRPC:**
```json
CreateUser {
  email: "maria@mail.com",
  password: "secret123",
  first_name: "María"
}
```

**Qué pasa, paso a paso:**

```
┌───────────────────────────────────────────────────────────┐
│ 1. RecoveryInterceptor: defer recover() (captura panics)  │
└────────────────┬──────────────────────────────────────────┘
                 ▼
┌───────────────────────────────────────────────────────────┐
│ 2. CorrelationIDInterceptor:                              │
│    - md.Get("x-correlation-id") → "" (no vino)            │
│    - id = uuid.NewString() → "a43683b0-..."               │
│    - ctx = apperr.WithCorrelationID(ctx, id)              │
└────────────────┬──────────────────────────────────────────┘
                 ▼
┌───────────────────────────────────────────────────────────┐
│ 3. LoggingInterceptor: start = now()                      │
└────────────────┬──────────────────────────────────────────┘
                 ▼
┌───────────────────────────────────────────────────────────┐
│ 4. UserHandler.CreateUser:                                │
│    cmd = CreateUserCmd{Email, Password, FirstName, ...}   │
│    → h.users.CreateUser(ctx, cmd)                         │
└────────────────┬──────────────────────────────────────────┘
                 ▼
┌───────────────────────────────────────────────────────────┐
│ 5. UserService.CreateUser:                                │
│    a. email.Normalize() → "maria@mail.com"                │
│    b. email.Validate()  → ok                              │
│    c. ValidatePasswordStrength("secret123") → ok          │
│    d. users.FindByEmail(ctx, email)                       │
└─────┬─────────────────────────────────────────────────────┘
      │                                                     
      ▼                                                     
┌─────────────────────────────────────┐                     
│ UserRepo.FindByEmail (postgres)     │                     
│ SELECT ... WHERE email = $1         │                     
│ → pgx.ErrNoRows → ErrUserNotFound   │                     
└─────┬───────────────────────────────┘                     
      │                                                     
      ▼ (vuelve al service)                                 
   e. errors.Is(err, ErrUserNotFound) → true → sigue        
   f. hasher.Hash("secret123")        → "$2a$10$..."        
   g. users.Save(ctx, user)                                 
      │                                                     
      ▼                                                     
┌─────────────────────────────────────┐                     
│ UserRepo.Save                       │                     
│ INSERT ... RETURNING id::text       │                     
│ → Postgres genera uuidv7()          │                     
│ → devuelve "0194f2e8-..."           │                     
└─────┬───────────────────────────────┘                     
      │                                                     
   h. user.ID = UserID("0194f2e8-...")                      
   i. cache.Set(ctx, user)                                  
      │                                                     
      ▼                                                     
┌─────────────────────────────────────┐                     
│ UserCache.Set (redis)               │                     
│ SET users_service:user:0194f2e8:... │                     
│     JSON... EX 900                  │                     
└─────┬───────────────────────────────┘                     
      │                                                     
      ▼ vuelve al handler                                   
┌───────────────────────────────────────────────────────────┐
│ 6. Handler: toProtoUser(u) → *pb.User                     │
│    return &pb.UserResponse{User: ...}, nil                │
└────────────────┬──────────────────────────────────────────┘
                 ▼
┌───────────────────────────────────────────────────────────┐
│ 7. LoggingInterceptor al final:                           │
│    log: "grpc method=/.../CreateUser code=OK dur=12ms     │
│          cid=a43683b0-..."                                │
└────────────────┬──────────────────────────────────────────┘
                 ▼
┌───────────────────────────────────────────────────────────┐
│ 8. Response viaja al gateway con header                   │
│    x-correlation-id: a43683b0-...                         │
└───────────────────────────────────────────────────────────┘
```

### 10.2 SearchUsers — flujo completo

**Request gRPC:**
```json
{
  "filterGroups": [{
    "filters": [
      {"propertyName": "active",    "operator": "EQ", "values": ["true"]},
      {"propertyName": "email",     "operator": "CONTAINS", "values": ["@gmail"]}
    ]
  }],
  "properties": ["email", "first_name"],
  "sorts": [{"propertyName": "created_at", "direction": "DESC"}],
  "limit": 10
}
```

**Qué pasa:**

```
Handler → search.RequestFromProto(req) → Request Go puro
       ↓
UserService.Search → delega al repo
       ↓
UserRepo.Search → BuildSearch(userSearchSchema, req)
       ↓
       SQL generado:
       ┌─────────────────────────────────────────────────────┐
       │ SELECT id::text AS __id,                            │
       │        created_at AS __created_at,                  │
       │        updated_at AS __updated_at,                  │
       │        (NOT active) AS __archived,                  │
       │        COUNT(*) OVER() AS __total,                  │
       │        email AS "email",                            │
       │        first_name AS "first_name"                   │
       │   FROM users                                        │
       │  WHERE (active = $1::boolean AND email ILIKE $2)    │
       │  ORDER BY created_at DESC                           │
       │  LIMIT $3 OFFSET $4                                 │
       └─────────────────────────────────────────────────────┘
       args: ["true", "%@gmail%", 10, 0]
       ↓
Postgres ejecuta → pgx.CollectRows(rows, pgx.RowToMap)
       ↓
       [
         {__id: "0194...", __created_at: <time>, __total: 3, email: "maria@gmail.com", first_name: "María"},
         {__id: "0195...", __created_at: <time>, __total: 3, email: "juan@gmail.com",  first_name: "Juan"},
         {__id: "0196...", __created_at: <time>, __total: 3, email: "ana@gmail.com",   first_name: "Ana"}
       ]
       ↓
Repo arma search.Response (Total=3, Results=3)
       ↓
search.ResponseToProto → *commonpb.SearchResponse
       ↓
Cliente recibe:
{
  "total": 3,
  "results": [
    {
      "id": "0194...",
      "properties": {"email": "maria@gmail.com", "first_name": "María"},
      "createdAt": "2026-04-22T10:00:00Z",
      "updatedAt": "2026-04-22T10:00:00Z",
      "archived": false
    },
    ...
  ],
  "paging": {"nextAfter": 3, "hasMore": false}
}
```

### 10.3 Error flow — email duplicado

```
CreateUser("maria@mail.com", ...)                       ← email ya existe
       ↓
UserService.CreateUser
       ↓
users.FindByEmail(ctx, email)                           ← devuelve User existente
       ↓
return nil, domain.ErrEmailTaken                        ← *apperr.Error
       ↓
Handler: apperr.ToGRPC(ctx, err)
       ↓
       errors.As(err, &ae) → ae = &apperr.Error{
         Kind: KindConflict,
         Code: "EMAIL_TAKEN",
         Message: "email already registered",
         Field: "email",
       }
       ↓
       transportFor(KindConflict) → codes.AlreadyExists, CATEGORY_CONFLICT
       ↓
       payload = &ErrorResponse{
         status: "error",
         message: "email already registered",
         errors: [{code:"EMAIL_TAKEN", message:"...", context:{propertyName:["email"]}}],
         category: CONFLICT,
         correlationId: "a43683b0-...",
       }
       ↓
       status.New(codes.AlreadyExists, "email...").WithDetails(payload)
       ↓
Cliente recibe:
  - gRPC code: AlreadyExists
  - Body del error:
{
  "status": "error",
  "message": "email already registered",
  "errors": [{
    "code": "EMAIL_TAKEN",
    "message": "email already registered",
    "context": {"propertyName": ["email"]}
  }],
  "category": "CONFLICT",
  "correlationId": "a43683b0-..."
}
```

---

## 11. Cómo extenderlo sin romper nada

### Agregar un nuevo error

**1 archivo:**
```go
// internal/core/domain/errors.go
var ErrSchoolInactive = apperr.NewPermissionDenied(
    "SCHOOL_INACTIVE", "school is disabled")
```

Devuélvelo desde donde quieras: `return domain.ErrSchoolInactive`. El envelope se arma solo.

### Agregar un nuevo método RPC

**Orden "de afuera hacia adentro":**

1. `proto/user.proto` → añade el `rpc` + mensajes.
2. `buf generate` → regenera stubs.
3. `ports/inbound.go` → añade al `UserUseCase`.
4. `ports/outbound.go` → si necesita algo nuevo del repo, declara la interfaz.
5. `service/user_service.go` → implementa el caso de uso.
6. `adapters/outbound/postgres/user_repo.go` → implementa si añadiste en outbound.
7. `adapters/inbound/grpc/user_handler.go` → recibe proto, llama al core, mapea.

### Agregar un nuevo campo filtrable en Search

**1 archivo:**
```go
// adapters/outbound/postgres/user_schema.go
Columns: map[string]SearchColumn{
    // ... existentes ...
    "new_field": {DBName: "new_field", Type: SearchTypeString,
                  Filterable: true, Sortable: true, Selectable: true},
},
```

Listo. El motor genérico lo descubre automáticamente.

### Agregar búsqueda a OTRA tabla (School, Exam, Order...)

Digamos tienes `ExamRepo` y quieres darle `SearchExams`. **3 pasos, 0 código duplicado:**

**Paso 1** — declara el schema (como `user_schema.go`):
```go
// adapters/outbound/postgres/exam_schema.go
var examSearchSchema = SearchSchema{
    Table: "exam", IDColumn: "id",
    CreatedCol: "created_at", UpdatedCol: "updated_at",
    DefaultLimit: 50, MaxLimit: 200,
    Columns: map[string]SearchColumn{
        "title":     {DBName: "title",        Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
        "score":     {DBName: "score",        Type: SearchTypeInt,    Filterable: true, Sortable: true, Selectable: true},
        "exam_type": {DBName: "exam_type_id", Type: SearchTypeUUID,   Filterable: true, Selectable: true},
    },
}
```

**Paso 2** — embebe `*SearchEngine` en el repo:
```go
// adapters/outbound/postgres/exam_repo.go
type ExamRepo struct {
    pool *pgxpool.Pool
    *SearchEngine           // ← hereda Search() automáticamente
}

func NewExamRepo(pool *pgxpool.Pool) *ExamRepo {
    return &ExamRepo{
        pool:         pool,
        SearchEngine: NewSearchEngine(pool, examSearchSchema),
    }
}
```

**Paso 3** — no hay paso 3. `ExamRepo.Search(ctx, req)` ya existe por promoción del método embebido.

Visual del sistema completo:

```
                    ┌─────────────────────────┐
                    │    *SearchEngine        │   ← definido UNA vez
                    │                         │
                    │    Search(ctx, req)     │
                    └───────────┬─────────────┘
                                │ embed en cada repo
          ┌─────────────────────┼─────────────────────┐
          ▼                     ▼                     ▼
   ┌──────────────┐      ┌─────────────┐      ┌─────────────┐
   │  UserRepo    │      │ SchoolRepo  │      │  ExamRepo   │
   │ + userSchema │      │ +schoolSch  │      │ + examSch   │
   │  .Search()   │      │  .Search()  │      │  .Search()  │
   └──────────────┘      └─────────────┘      └─────────────┘
        promoción de método — mismo código ejecutándose
```

### Cambiar la DB (Postgres → Mongo)

**3 cambios:**
1. Crea `internal/adapters/outbound/mongo/user_repo.go` implementando `UserRepository`.
2. En `cmd/main.go`, reemplaza `postgresadapter.NewUserRepo(pool)` por `mongoadapter.NewUserRepo(client)`.
3. Ajusta `config.go` si el DSN cambia.

Ni el core, ni el handler, ni el proto se tocan.

### Agregar un nuevo microservicio (ej: `exams_service`)

1. Copiar `buf.yaml`, estructura de carpetas.
2. Importar `proto/common/*.proto` para los tipos compartidos.
3. Reutilizar `search_builder.go` (idealmente moverlo a un módulo Go compartido).
4. Implementar schema de tu tabla + handler.

El patrón es idéntico. Lo que hagas una vez te sirve para todos los demás.

---

## 🎯 Resumen de las ideas en una tabla

| Concepto | Dónde vive | Por qué |
|----------|-----------|---------|
| Validaciones de negocio | `domain/*.go` | Son parte de la entidad, no del transporte |
| Casos de uso | `service/*.go` | Orquestación de dominio + puertos |
| Puertos inbound (driving) | `ports/inbound.go` | Lo que el mundo le pide al core (UseCases + Cmd) |
| Puertos outbound (driven) | `ports/outbound.go` | Lo que el core pide al mundo (Repos, Cache, Hasher) |
| SQL real | `adapters/outbound/postgres/*.go` | Único lugar con imports de pgx |
| Conversión proto ↔ dominio | `adapters/inbound/grpc/*_mapper.go` | SRP: un archivo por razón de cambio |
| Traducción de errores | `shared/apperr/apperr.go` | Un switch global, no 15 por handler |
| Filtros + SQL | `adapters/outbound/postgres/search_builder.go` | Construye SQL parametrizado |
| Ejecución búsqueda | `adapters/outbound/postgres/search_engine.go` | Genérico: cualquier repo lo embebe |
| Schema por tabla | `adapters/outbound/postgres/<tabla>_schema.go` | Whitelist de columnas filtrables |
| Arranque + wiring | `cmd/main.go` | Composition Root único |
| Config | `config/config.go` | ENV vars tipadas |

Si aprendes dónde va cada cosa, **escribir un servicio nuevo se convierte en mecánico**: mismo esqueleto, cambia solo el dominio.

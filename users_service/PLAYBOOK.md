# `users_service` — Playbook de desarrollo

Guía práctica para **seguir escribiendo código solo** respetando el patrón ya establecido (Hexagonal + **CQRS** + SOLID + envelope único de error + search HubSpot-style).

> 📐 **Antes de empezar**, leé [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — el documento canónico que rige a TODOS los microservicios. Este PLAYBOOK contiene las recetas concretas; ARCHITECTURE define las reglas.

Todo lo que vas a hacer cae en **una de 7 recetas**. Sigue los pasos en orden y el código queda consistente con el resto.

> ℹ️ **Nota CQRS**: cuando estas recetas dicen "service", referite a **`internal/core/command/`** si el caso de uso muta estado, o **`internal/core/query/`** si solo lee. La separación es por archivo, no por base de datos. Regla rápida: si tu método tiene un `INSERT/UPDATE/DELETE`, un `cache.Delete`, o un `audit.Record` → command. Si solo lee → query.

---

## 📑 Índice

- [0. Las 7 reglas de oro](#0-las-7-reglas-de-oro)
- [1. Orden de trabajo: de afuera hacia adentro](#1-orden-de-trabajo-de-afuera-hacia-adentro)
- [**R1** — Agregar un CRUD completo (nueva entidad)](#r1--agregar-un-crud-completo-nueva-entidad)
- [**R2** — Agregar un método a un service existente](#r2--agregar-un-método-a-un-service-existente)
- [**R3** — Agregar un error de negocio](#r3--agregar-un-error-de-negocio)
- [**R4** — Agregar un campo filtrable a una búsqueda](#r4--agregar-un-campo-filtrable-a-una-búsqueda)
- [**R5** — Cambiar una regla de negocio existente](#r5--cambiar-una-regla-de-negocio-existente)
- [**R6** — Agregar búsqueda a una entidad que no la tenía](#r6--agregar-búsqueda-a-una-entidad-que-no-la-tenía)
- [**R7** — Cambiar un adapter (Postgres → X, bcrypt → argon2, ...)](#r7--cambiar-un-adapter-postgres--x-bcrypt--argon2-)
- [Anti-patrones (qué NO hacer)](#anti-patrones-qué-no-hacer)
- [Checklist antes de mergear](#checklist-antes-de-mergear)

---

## 0. Las 7 reglas de oro

Estas son inviolables. Si dudas, detente y relee.

1. **Las dependencias apuntan siempre HACIA EL CORE.** El core (`internal/core/`) nunca importa `mssql`, `redis`, `grpc`, `bcrypt`, `jwt`. Si lo hace, el patrón está roto.
2. **Commands y Queries están separados físicamente.** No hay un solo struct con `Create()` y `Get()` juntos. Distintos archivos (`core/command/user.go` vs `core/query/user.go`), distintos handlers, distintos puertos inbound (`UserCommands`, `UserQueries`).
3. **Los errores del sistema son `*apperr.Error` tipados.** Nunca `errors.New("...")` para errores que van al cliente. Vive en `domain/errors.go` o factories en `shared/`.
4. **El handler gRPC es tonto.** Traduce proto ↔ DTO de comando/query, llama al port inbound, traduce dominio ↔ proto. **Cero validación o lógica de negocio ahí.**
5. **Toda columna que se expone por búsqueda DEBE estar en un schema.** La whitelist en `*_schema.go` es la fuente de verdad. `password_hash` o cualquier campo sensible NO se declara → no se puede filtrar/devolver.
6. **`cmd/main.go` es el único que construye cosas concretas.** El resto recibe interfaces por constructor. Ni `sql.Open(...)` ni `redis.NewClient(...)` fuera de ahí.
7. **Cada servicio es autónomo.** No comparte código Go con otros servicios. Si exams_service necesita `migrator` o `mssql`, copia el archivo. Duplicación se acepta a cambio de independencia total.

---

## 1. Orden de trabajo: de afuera hacia adentro

Cuando agregas algo nuevo, sigue **siempre este orden**. Así al final todo encaja solo:

```
  1. PROTO              (define el contrato)
         ↓
  2. DOMAIN             (entidades + errores)
         ↓
  3. PORTS              (interfaces que el core ofrece / necesita)
         ↓
  4. COMMAND o QUERY    (lógica de negocio — elegir según CQRS)
         ↓
  5. ADAPTERS OUT       (implementan ports outbound: mssql, redis, bcrypt, ...)
         ↓
  6. ADAPTERS IN        (gRPC handler: implementa el pb, llama al port inbound)
         ↓
  7. MAIN               (conecta todo en cmd/main.go)
         ↓
  8. BUILD + TEST
```

**¿Por qué este orden?** Porque cada capa declara **contratos** antes que la siguiente **use** esos contratos. Si escribes el handler primero sin el port, no tienes a quién llamar. Si escribes el adapter sin el port, no sabes qué implementar.

---

## R1 — Agregar un CRUD completo (nueva entidad)

> **Ejemplo corrido:** vamos a agregar la entidad **`APIKey`** — claves API que un usuario puede generar para integrarse por API. Ejercita todas las capas.

Campos que queremos:
- `id` UUID
- `user_id` UUID (FK a `users`)
- `name` string (ej: "mobile app")
- `token_hash` string (nunca se expone)
- `last_used_at` timestamp NULL
- `revoked_at` timestamp NULL
- `created_at` timestamp

Operaciones:
- `CreateAPIKey(userID, name)` → devuelve el token en claro UNA vez; guarda el hash
- `GetAPIKey(id)`
- `RevokeAPIKey(id)` → pone `revoked_at = NOW()`
- `SearchAPIKeys(filtros)` → lista con la búsqueda HubSpot-style

### Paso 1 — Crear la tabla en la DB

Archivo nuevo: `db/db/imports/usuarios/09_api_key.sql`

```sql
USE db_users;  -- PG: se conecta con -d db_users

CREATE TABLE IF NOT EXISTS api_key (
  id            UUID         NOT NULL DEFAULT uuidv7() PRIMARY KEY,
  user_id       UUID         NOT NULL,
  name          VARCHAR(120) NOT NULL,
  token_hash    VARCHAR(255) NOT NULL,
  last_used_at  TIMESTAMPTZ  NULL,
  revoked_at    TIMESTAMPTZ  NULL,
  created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  CONSTRAINT fk_api_key_user FOREIGN KEY (user_id)
    REFERENCES users (id) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX idx_api_key_user    ON api_key (user_id);
CREATE INDEX idx_api_key_revoked ON api_key (revoked_at);
```

Correr: `.\ejecutar_todo.ps1 -Usuario postgres -Password "0510"`.

### Paso 2 — Dominio

Archivo nuevo: `internal/core/domain/api_key.go`

```go
package domain

import "time"

type APIKeyID string

type APIKey struct {
    ID           APIKeyID
    UserID       UserID
    Name         string
    TokenHash    string     // NUNCA se envía al cliente
    LastUsedAt   *time.Time
    RevokedAt    *time.Time
    CreatedAt    time.Time
}

func (k *APIKey) IsRevoked() bool {
    return k.RevokedAt != nil
}
```

**Por qué `APIKeyID` separado:** Value Object tipado. No lo puedes confundir con `UserID`.

**Por qué `IsRevoked()` aquí:** es una pregunta sobre la entidad, vive con ella. NO en el handler, NO en el service.

Y al archivo existente `internal/core/domain/errors.go`, agrega:

```go
var (
    // ... existentes ...
    ErrAPIKeyNotFound = apperr.NewNotFound("API_KEY_NOT_FOUND", "api key not found")
    ErrAPIKeyRevoked  = apperr.NewPermissionDenied("API_KEY_REVOKED", "api key has been revoked")
)
```

### Paso 3 — Ports outbound (qué necesita el core)

Agrega al archivo `internal/core/ports/outbound.go`:

```go
type APIKeyReader interface {
    FindByID(ctx context.Context, id domain.APIKeyID) (*domain.APIKey, error)
    FindByTokenHash(ctx context.Context, hash string) (*domain.APIKey, error)
}

type APIKeyWriter interface {
    Save(ctx context.Context, k *domain.APIKey) (domain.APIKeyID, error)
    Revoke(ctx context.Context, id domain.APIKeyID) error
    TouchLastUsed(ctx context.Context, id domain.APIKeyID) error
}

type APIKeySearcher interface {
    Search(ctx context.Context, req search.Request) (*search.Response, error)
}

type APIKeyRepository interface {
    APIKeyReader
    APIKeyWriter
    APIKeySearcher
}
```

**Por qué 3 interfaces segregadas:** mañana un servicio read-only puede depender solo de `APIKeyReader`. ISP satisfecho desde el día 1.

### Paso 4 — Ports inbound (qué ofrece el core)

Agrega a `internal/core/ports/inbound.go`:

```go
type APIKeyUseCase interface {
    Create(ctx context.Context, cmd CreateAPIKeyCmd) (*domain.APIKey, string, error)  // devuelve (entity, plain_token, err)
    Get(ctx context.Context, id domain.APIKeyID) (*domain.APIKey, error)
    Revoke(ctx context.Context, id domain.APIKeyID) error
    Search(ctx context.Context, req search.Request) (*search.Response, error)
}

type CreateAPIKeyCmd struct {
    UserID domain.UserID
    Name   string
}
```

**Por qué `Create` devuelve 3 cosas:** la entidad + el token en claro (una sola vez) + error. El token en claro **nunca se guarda**, solo se lo das al cliente una vez al crear.

### Paso 5 — Definir el contrato proto

Nuevo archivo: `proto/api_key.proto`

```proto
syntax = "proto3";
package users.v1;
option go_package = "users_service/proto/gen;userspb";

import "google/protobuf/timestamp.proto";
import "common/search.proto";

service APIKeyService {
  rpc CreateAPIKey  (CreateAPIKeyRequest) returns (CreateAPIKeyResponse);
  rpc GetAPIKey     (GetAPIKeyRequest)    returns (APIKeyResponse);
  rpc RevokeAPIKey  (RevokeAPIKeyRequest) returns (EmptyResponse);
  rpc SearchAPIKeys (common.v1.SearchRequest) returns (common.v1.SearchResponse);
}

message APIKey {
  string id           = 1;
  string user_id      = 2;
  string name         = 3;
  // NO incluir token_hash
  google.protobuf.Timestamp last_used_at = 4;
  google.protobuf.Timestamp revoked_at   = 5;
  google.protobuf.Timestamp created_at   = 6;
}

message CreateAPIKeyRequest  { string user_id = 1; string name = 2; }
message CreateAPIKeyResponse { APIKey api_key = 1; string plain_token = 2; }  // token en claro, solo una vez
message GetAPIKeyRequest     { string id = 1; }
message RevokeAPIKeyRequest  { string id = 1; }
message APIKeyResponse       { APIKey api_key = 1; }
```

**Regla de oro en proto:** **nunca** expongas datos sensibles (password_hash, token_hash, pin). Si accidentalmente los pones, `buf` no te protege — tú debes revisarlo.

### Paso 6 — Generar stubs

```bash
cd users_service
buf generate
```

Esto crea `proto/gen/api_key.pb.go` y `api_key_grpc.pb.go`. Los revisas pero nunca los editas a mano.

### Paso 7 — Implementar el service (lógica de negocio)

Nuevo archivo: `internal/core/service/api_key_service.go`

```go
package service

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "strings"
    "time"

    "users_service/internal/core/domain"
    "users_service/internal/core/ports"
    "users_service/internal/shared/search"
)

type APIKeyService struct {
    keys   ports.APIKeyRepository
    users  ports.UserReader       // para validar que el user exista
    hasher ports.PasswordHasher   // reutilizamos el mismo hasher
}

var _ ports.APIKeyUseCase = (*APIKeyService)(nil)

func NewAPIKeyService(keys ports.APIKeyRepository, users ports.UserReader, hasher ports.PasswordHasher) *APIKeyService {
    return &APIKeyService{keys: keys, users: users, hasher: hasher}
}

func (s *APIKeyService) Create(ctx context.Context, cmd ports.CreateAPIKeyCmd) (*domain.APIKey, string, error) {
    if strings.TrimSpace(cmd.Name) == "" {
        return nil, "", apperr.NewValidation("INVALID_NAME", "name is required", "name")
    }

    // Validar que el usuario existe.
    if _, err := s.users.FindByID(ctx, cmd.UserID); err != nil {
        return nil, "", err  // propaga ErrUserNotFound
    }

    // Generar token aleatorio (32 bytes = 64 chars hex).
    buf := make([]byte, 32)
    if _, err := rand.Read(buf); err != nil {
        return nil, "", err
    }
    plainToken := hex.EncodeToString(buf)

    // Hashear antes de guardar. Solo el hash va a la DB.
    hash, err := s.hasher.Hash(plainToken)
    if err != nil {
        return nil, "", err
    }

    k := &domain.APIKey{
        UserID:    cmd.UserID,
        Name:      strings.TrimSpace(cmd.Name),
        TokenHash: hash,
        CreatedAt: time.Now(),
    }
    id, err := s.keys.Save(ctx, k)
    if err != nil {
        return nil, "", err
    }
    k.ID = id
    return k, plainToken, nil  // el plainToken se devuelve UNA vez
}

func (s *APIKeyService) Get(ctx context.Context, id domain.APIKeyID) (*domain.APIKey, error) {
    return s.keys.FindByID(ctx, id)
}

func (s *APIKeyService) Revoke(ctx context.Context, id domain.APIKeyID) error {
    return s.keys.Revoke(ctx, id)
}

func (s *APIKeyService) Search(ctx context.Context, req search.Request) (*search.Response, error) {
    return s.keys.Search(ctx, req)
}
```

**Por qué hasheamos el token antes de guardar:**
- Si la DB se filtra, los tokens no sirven (son hashes).
- Igual que con passwords: nunca guardes secrets en claro.

**Por qué `_ ports.APIKeyUseCase = (*APIKeyService)(nil):` — si olvidas un método, **no compila**. Test gratis.

### Paso 8 — Adapter Postgres

Archivo nuevo: `internal/adapters/outbound/postgres/api_key_repo.go`

```go
package postgresadapter

import (
    "context"
    "errors"
    "time"

    "users_service/internal/core/domain"
    "users_service/internal/core/ports"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
)

type APIKeyRepo struct {
    pool *pgxpool.Pool
    *SearchEngine    // ← gana Search() por embedding
}

var _ ports.APIKeyRepository = (*APIKeyRepo)(nil)

func NewAPIKeyRepo(pool *pgxpool.Pool) *APIKeyRepo {
    return &APIKeyRepo{
        pool:         pool,
        SearchEngine: NewSearchEngine(pool, apiKeySearchSchema),
    }
}

const apiKeyCols = `id::text, user_id::text, name, token_hash, last_used_at, revoked_at, created_at`

func (r *APIKeyRepo) Save(ctx context.Context, k *domain.APIKey) (domain.APIKeyID, error) {
    const q = `
        INSERT INTO api_key (user_id, name, token_hash)
        VALUES ($1::uuid, $2, $3)
        RETURNING id::text`
    var id string
    err := r.pool.QueryRow(ctx, q, string(k.UserID), k.Name, k.TokenHash).Scan(&id)
    if err != nil { return "", err }
    return domain.APIKeyID(id), nil
}

func (r *APIKeyRepo) FindByID(ctx context.Context, id domain.APIKeyID) (*domain.APIKey, error) {
    row := r.pool.QueryRow(ctx,
        `SELECT `+apiKeyCols+` FROM api_key WHERE id = $1::uuid`, string(id))
    return scanAPIKey(row)
}

func (r *APIKeyRepo) FindByTokenHash(ctx context.Context, hash string) (*domain.APIKey, error) {
    row := r.pool.QueryRow(ctx,
        `SELECT `+apiKeyCols+` FROM api_key WHERE token_hash = $1`, hash)
    return scanAPIKey(row)
}

func (r *APIKeyRepo) Revoke(ctx context.Context, id domain.APIKeyID) error {
    tag, err := r.pool.Exec(ctx,
        `UPDATE api_key SET revoked_at = NOW() WHERE id = $1::uuid AND revoked_at IS NULL`,
        string(id))
    if err != nil { return err }
    if tag.RowsAffected() == 0 { return domain.ErrAPIKeyNotFound }
    return nil
}

func (r *APIKeyRepo) TouchLastUsed(ctx context.Context, id domain.APIKeyID) error {
    _, err := r.pool.Exec(ctx,
        `UPDATE api_key SET last_used_at = NOW() WHERE id = $1::uuid`, string(id))
    return err
}

type rowScanner2 interface{ Scan(dest ...any) error }

func scanAPIKey(row rowScanner2) (*domain.APIKey, error) {
    var (
        k          domain.APIKey
        idStr      string
        userIDStr  string
        lastUsed   *time.Time
        revokedAt  *time.Time
    )
    err := row.Scan(&idStr, &userIDStr, &k.Name, &k.TokenHash, &lastUsed, &revokedAt, &k.CreatedAt)
    if errors.Is(err, pgx.ErrNoRows) { return nil, domain.ErrAPIKeyNotFound }
    if err != nil { return nil, err }
    k.ID = domain.APIKeyID(idStr)
    k.UserID = domain.UserID(userIDStr)
    k.LastUsedAt = lastUsed
    k.RevokedAt = revokedAt
    return &k, nil
}
```

Archivo nuevo: `internal/adapters/outbound/postgres/api_key_schema.go`

```go
package postgresadapter

// Whitelist: qué se puede filtrar/ordenar/devolver.
// token_hash NO está → imposible exponerlo.
var apiKeySearchSchema = SearchSchema{
    Table:        "api_key",
    IDColumn:     "id",
    CreatedCol:   "created_at",
    UpdatedCol:   "created_at",    // api_key no tiene updated_at; usa created_at
    ArchivedExpr: "revoked_at IS NOT NULL",
    DefaultLimit: 50,
    MaxLimit:     200,
    Columns: map[string]SearchColumn{
        "user_id":      {DBName: "user_id",      Type: SearchTypeUUID,      Filterable: true, Sortable: false, Selectable: true},
        "name":         {DBName: "name",         Type: SearchTypeString,    Filterable: true, Sortable: true,  Selectable: true},
        "last_used_at": {DBName: "last_used_at", Type: SearchTypeTimestamp, Filterable: true, Sortable: true,  Selectable: true},
        "revoked_at":   {DBName: "revoked_at",   Type: SearchTypeTimestamp, Filterable: true, Sortable: true,  Selectable: true},
        "created_at":   {DBName: "created_at",   Type: SearchTypeTimestamp, Filterable: true, Sortable: true,  Selectable: true},
    },
}
```

### Paso 9 — Handler gRPC

Archivo nuevo: `internal/adapters/inbound/grpc/api_key_handler.go`

```go
package grpchandler

import (
    "context"

    "users_service/internal/core/domain"
    "users_service/internal/core/ports"
    "users_service/internal/shared/apperr"
    "users_service/internal/shared/search"
    pb "users_service/proto/gen"
    commonpb "users_service/proto/gen/common"
)

type APIKeyHandler struct {
    pb.UnimplementedAPIKeyServiceServer
    keys ports.APIKeyUseCase
}

func NewAPIKeyHandler(keys ports.APIKeyUseCase) *APIKeyHandler {
    return &APIKeyHandler{keys: keys}
}

func (h *APIKeyHandler) CreateAPIKey(ctx context.Context, req *pb.CreateAPIKeyRequest) (*pb.CreateAPIKeyResponse, error) {
    k, token, err := h.keys.Create(ctx, ports.CreateAPIKeyCmd{
        UserID: domain.UserID(req.GetUserId()),
        Name:   req.GetName(),
    })
    if err != nil {
        return nil, apperr.ToGRPC(ctx, err)
    }
    return &pb.CreateAPIKeyResponse{
        ApiKey:     toProtoAPIKey(k),
        PlainToken: token,   // ← única vez que se envía
    }, nil
}

func (h *APIKeyHandler) GetAPIKey(ctx context.Context, req *pb.GetAPIKeyRequest) (*pb.APIKeyResponse, error) {
    k, err := h.keys.Get(ctx, domain.APIKeyID(req.GetId()))
    if err != nil {
        return nil, apperr.ToGRPC(ctx, err)
    }
    return &pb.APIKeyResponse{ApiKey: toProtoAPIKey(k)}, nil
}

func (h *APIKeyHandler) RevokeAPIKey(ctx context.Context, req *pb.RevokeAPIKeyRequest) (*pb.EmptyResponse, error) {
    if err := h.keys.Revoke(ctx, domain.APIKeyID(req.GetId())); err != nil {
        return nil, apperr.ToGRPC(ctx, err)
    }
    return &pb.EmptyResponse{}, nil
}

func (h *APIKeyHandler) SearchAPIKeys(ctx context.Context, req *commonpb.SearchRequest) (*commonpb.SearchResponse, error) {
    resp, err := h.keys.Search(ctx, search.RequestFromProto(req))
    if err != nil {
        return nil, apperr.ToGRPC(ctx, err)
    }
    return search.ResponseToProto(resp)
}
```

Archivo nuevo: `internal/adapters/inbound/grpc/api_key_mapper.go`

```go
package grpchandler

import (
    "users_service/internal/core/domain"
    pb "users_service/proto/gen"
)

func toProtoAPIKey(k *domain.APIKey) *pb.APIKey {
    if k == nil { return nil }
    return &pb.APIKey{
        Id:         string(k.ID),
        UserId:     string(k.UserID),
        Name:       k.Name,
        // token_hash NUNCA aquí
        LastUsedAt: tsOrNil(k.LastUsedAt),
        RevokedAt:  tsOrNil(k.RevokedAt),
        CreatedAt:  timestamppb.New(k.CreatedAt),
    }
}
```

### Paso 10 — Wiring en `main.go`

En `cmd/main.go`, agrega tras los repos existentes:

```go
// ---------- 2. ADAPTERS OUTBOUND ----------
userRepo  := postgresadapter.NewUserRepo(pool)
permRepo  := postgresadapter.NewPermissionRepo(pool)
apiKeyRepo := postgresadapter.NewAPIKeyRepo(pool)    // ← NEW
cache     := redisadapter.NewUserCache(rdb, cfg.CacheTTL)
hasher    := bcryptadapter.New(cfg.BcryptCost)

// ---------- 3. CORE ----------
userSvc   := service.NewUserService(userRepo, permRepo, cache, hasher)
permSvc   := service.NewPermissionService(userRepo, permRepo)
apiKeySvc := service.NewAPIKeyService(apiKeyRepo, userRepo, hasher)   // ← NEW

// ---------- 4. ADAPTERS INBOUND ----------
userHandler   := grpchandler.NewUserHandler(userSvc, permSvc)
apiKeyHandler := grpchandler.NewAPIKeyHandler(apiKeySvc)              // ← NEW

// ---------- 5. SERVER ----------
pb.RegisterUserServiceServer(s, userHandler)
pb.RegisterAPIKeyServiceServer(s, apiKeyHandler)                       // ← NEW
```

### Paso 11 — Compilar y probar

```bash
go mod tidy
go build ./...
go vet ./...

# Test rápido con grpcurl:
grpcurl -plaintext -d '{"user_id":"<uuid-maria>", "name":"mi key"}' \
    localhost:50051 users.v1.APIKeyService/CreateAPIKey
```

### ✅ Checklist R1

- [ ] Migración SQL creada y corrida
- [ ] Dominio con VOs tipados (`APIKeyID` separado de string)
- [ ] Errores nuevos en `domain/errors.go` como `apperr.NewX(...)`
- [ ] Ports outbound segregados (Reader/Writer/Searcher) + composite
- [ ] Port inbound (UseCase) definido
- [ ] Proto generado sin campos sensibles
- [ ] Service implementa el UseCase + `var _` guard
- [ ] Adapter Postgres con embed `*SearchEngine`
- [ ] Schema whitelist sin columnas sensibles
- [ ] Handler llama `apperr.ToGRPC(ctx, err)` en cada error
- [ ] `main.go` actualizado con instancias nuevas
- [ ] `go build ./...` sin errores
- [ ] `go vet ./...` sin warnings

---

## R2 — Agregar un método a un service existente

> Ejemplo: "quiero `ListUsersBySchool(schoolID)` en `UserService`".

**4 pasos, 4 archivos:**

### 1. Proto: agrega el RPC

```proto
// proto/user.proto
service UserService {
  // ...
  rpc ListUsersBySchool (ListBySchoolRequest) returns (ListUsersResponse);
}
message ListBySchoolRequest { string school_id = 1; }
message ListUsersResponse   { repeated User users = 1; }
```

Genera: `buf generate`.

### 2. Port inbound: añade el método

```go
// internal/core/ports/inbound.go
type UserUseCase interface {
    // ... existentes ...
    ListBySchool(ctx context.Context, schoolID domain.SchoolID) ([]*domain.User, error)
}
```

### 3. Port outbound (si necesitas nuevo método en el repo)

```go
// internal/core/ports/outbound.go
type UserReader interface {
    // ... existentes ...
    FindBySchool(ctx context.Context, schoolID domain.SchoolID) ([]*domain.User, error)
}
```

### 4. Implementar en service + repo + handler

```go
// internal/core/service/user_service.go
func (s *UserService) ListBySchool(ctx, id SchoolID) ([]*User, error) {
    return s.users.FindBySchool(ctx, id)
}

// internal/adapters/outbound/postgres/user_repo.go
func (r *UserRepo) FindBySchool(ctx, id SchoolID) ([]*User, error) {
    rows, err := r.pool.Query(ctx, `SELECT ... WHERE school_id = $1::uuid`, string(id))
    // scan...
}

// internal/adapters/inbound/grpc/user_handler.go
func (h *UserHandler) ListUsersBySchool(ctx, req) (*pb.ListUsersResponse, error) {
    users, err := h.users.ListBySchool(ctx, SchoolID(req.GetSchoolId()))
    if err != nil { return nil, apperr.ToGRPC(ctx, err) }
    protoUsers := make([]*pb.User, len(users))
    for i, u := range users { protoUsers[i] = toProtoUser(u) }
    return &pb.ListUsersResponse{Users: protoUsers}, nil
}
```

**Regla:** si vas agregar búsqueda con filtros, **NO inventes un método nuevo** — usa el `SearchUsers` que ya existe y pásale filtros. Solo crea métodos dedicados si la consulta es fija y repetida.

---

## R3 — Agregar un error de negocio

**1 archivo, 1 línea.**

```go
// internal/core/domain/errors.go
var (
    // ... existentes ...
    ErrSchoolInactive = apperr.NewPermissionDenied(
        "SCHOOL_INACTIVE", "school is disabled")
)
```

Úsalo desde cualquier service:
```go
if !school.Active {
    return nil, domain.ErrSchoolInactive
}
```

El cliente recibe automáticamente:
```json
{
  "status": "error",
  "message": "school is disabled",
  "errors": [{"code":"SCHOOL_INACTIVE","message":"school is disabled"}],
  "category": "PERMISSION_DENIED",
  "correlationId": "..."
}
```

**No tocas `mapper.go`, ni el handler, ni nada más.** El sistema ya sabe cómo traducirlo.

### Si el error tiene contexto dinámico

Usa un factory en vez de un singleton:

```go
// internal/core/domain/errors.go
func ErrQuotaExceeded(limit int) *apperr.Error {
    return apperr.NewPermissionDenied("QUOTA_EXCEEDED",
        fmt.Sprintf("quota of %d reached", limit))
}
```

Y llámalo: `return nil, domain.ErrQuotaExceeded(100)`.

---

## R4 — Agregar un campo filtrable a una búsqueda

**1 archivo, 1 línea.**

Digamos quieres poder filtrar usuarios por `phone`:

1. Asegúrate de que la columna existe en la DB (`ALTER TABLE users ADD COLUMN phone ...`).
2. Agrega al schema:

```go
// internal/adapters/outbound/postgres/user_schema.go
Columns: map[string]SearchColumn{
    // ... existentes ...
    "phone": {DBName: "phone", Type: SearchTypeString, Filterable: true, Sortable: false, Selectable: true},
},
```

Listo. Ya puedes hacer:
```json
{
  "filterGroups": [{
    "filters": [{"propertyName": "phone", "operator": "CONTAINS", "values": ["555"]}]
  }]
}
```

### Si no quieres que se devuelva pero sí filtrar

```go
"phone": {DBName: "phone", Type: SearchTypeString, Filterable: true, Sortable: false, Selectable: false},
```

`Selectable: false` → se puede usar en filtros pero nunca aparece en `properties` de la respuesta. Útil para campos semi-sensibles.

---

## R5 — Cambiar una regla de negocio existente

> Ejemplo: "ahora el password debe tener 12 caracteres, no 8".

**1 archivo: `internal/core/domain/user.go`**

```go
func ValidatePasswordStrength(plain string) error {
    if len(plain) < 12 {   // ← cambió de 8 a 12
        return ErrWeakPassword
    }
    return nil
}
```

Y actualiza el mensaje del error si tiene el número:

```go
// internal/core/domain/errors.go
ErrWeakPassword = apperr.NewValidation("WEAK_PASSWORD",
    "password must have at least 12 characters", "password"),
```

**Por qué NO tocar el handler:** la regla vive en el dominio, es una política del negocio. El handler solo pasa strings — le da igual cuántos chars mínimos tienen.

**Anti-patrón común:** agregar `if len(req.Password) < 12` en el handler. **MAL**. Eso duplica la regla: si mañana agregas REST o CLI, cada handler tiene la suya y se desincronizan.

---

## R6 — Agregar búsqueda a una entidad que no la tenía

> Ejemplo: `SchoolRepo` hoy solo tiene `FindByID`. Quieres `SearchSchools`.

**3 archivos:**

### 1. Schema (nuevo archivo)

```go
// internal/adapters/outbound/postgres/school_schema.go
package postgresadapter

var schoolSearchSchema = SearchSchema{
    Table:        "school",
    IDColumn:     "id",
    CreatedCol:   "created_at",
    UpdatedCol:   "updated_at",
    ArchivedExpr: "NOT active",
    DefaultLimit: 50,
    MaxLimit:     200,
    Columns: map[string]SearchColumn{
        "name":    {DBName: "name",    Type: SearchTypeString, Filterable: true, Sortable: true, Selectable: true},
        "active":  {DBName: "active",  Type: SearchTypeBool,   Filterable: true, Selectable: true},
        "user_id": {DBName: "user_id", Type: SearchTypeUUID,   Filterable: true, Selectable: true},
    },
}
```

### 2. Embed `*SearchEngine` en el repo

```go
// internal/adapters/outbound/postgres/school_repo.go
type SchoolRepo struct {
    pool *pgxpool.Pool
    *SearchEngine        // ← nueva línea
}

func NewSchoolRepo(pool *pgxpool.Pool) *SchoolRepo {
    return &SchoolRepo{
        pool:         pool,
        SearchEngine: NewSearchEngine(pool, schoolSearchSchema),   // ← nueva línea
    }
}
```

### 3. Exponer el port + RPC

```go
// internal/core/ports/outbound.go — agregar:
type SchoolRepository interface {
    FindByID(...) 
    Search(ctx, req search.Request) (*search.Response, error)   // ← nuevo
}
```

```go
// internal/core/ports/inbound.go — crear:
type SchoolUseCase interface {
    Search(ctx, req search.Request) (*search.Response, error)
}
```

Y en proto + handler + main, lo de siempre (R1 minimizado a solo el método nuevo).

**Tiempo total:** ~15 minutos. El motor hace todo el trabajo.

---

## R7 — Cambiar un adapter (Postgres → X, bcrypt → argon2, ...)

> Ejemplo: cambiar `bcrypt` por `argon2`.

**Pasos:**

### 1. Crear el nuevo adapter

Nuevo archivo: `internal/adapters/outbound/argon2/hasher.go`

```go
package argon2adapter

import (
    "users_service/internal/core/ports"
    "golang.org/x/crypto/argon2"
)

type Hasher struct { /* params */ }

var _ ports.PasswordHasher = (*Hasher)(nil)

func New(params ...) *Hasher { /* ... */ }
func (h *Hasher) Hash(plain string) (string, error) { /* ... */ }
func (h *Hasher) Compare(hashed, plain string) error { /* ... */ }
```

### 2. Swap en `main.go`

```go
// cmd/main.go
// Cambiar:
hasher := bcryptadapter.New(cfg.BcryptCost)
// Por:
hasher := argon2adapter.New(cfg.Argon2Params)
```

### 3. Listo

**Nada más se toca.** El core, los services, los handlers, el proto — todo sigue intacto. Esto es lo que el patrón hexagonal promete y cumple.

### La misma receta sirve para:

| Cambio | Archivos nuevos | Archivos tocados |
|--------|-----------------|------------------|
| Postgres → Mongo | `adapters/outbound/mongo/*.go` (3 repos) | `main.go` (3 líneas) |
| Redis → Memcached | `adapters/outbound/memcached/user_cache.go` | `main.go` (1 línea) |
| bcrypt → argon2 | `adapters/outbound/argon2/hasher.go` | `main.go` (1 línea) |
| gRPC → REST (agregar) | `adapters/inbound/http/*.go` | `main.go` (levantar un http.Server) |

---

## Anti-patrones (qué NO hacer)

### ❌ Poner validación en el handler

```go
// MAL
func (h *UserHandler) CreateUser(ctx, req) ... {
    if len(req.Password) < 8 {  // ← NO! esto va en el service/domain
        return nil, ...
    }
    // ...
}
```

**Por qué mal:** si mañana sumas HTTP, cada handler duplica la regla. Se desincronizan.

**Correcto:** validación en `domain/user.go::ValidatePasswordStrength`.

### ❌ Importar pgx/redis/bcrypt desde el core

```go
// internal/core/service/user_service.go
import "github.com/jackc/pgx/v5"   // ← NO
```

**Por qué mal:** el core deja de ser testeable sin Docker. Pierde toda la ventaja del patrón.

**Correcto:** el core solo importa `domain`, `ports`, `shared/`. Verifícalo con:
```bash
grep -r "pgx\|redis\|bcrypt\|grpc" internal/core/
# debe salir vacío
```

### ❌ Errores genéricos con `errors.New` o `fmt.Errorf`

```go
// MAL
return errors.New("email ya existe")
```

**Por qué mal:** el mapper no lo clasifica → el cliente recibe un `500 Internal`. Pierdes la categoría, el código, el field.

**Correcto:** declara en `domain/errors.go` como `apperr.NewX(...)` o úsalo inline si es único.

### ❌ Escribir SQL fuera de `adapters/outbound/postgres/`

```go
// internal/core/service/user_service.go
db.Query("SELECT * FROM users")   // ← NO
```

**Por qué mal:** acoplas el core a Postgres. Se rompe todo el patrón.

**Correcto:** declara el método en el port outbound, impleméntalo en el repo, el service solo llama al port.

### ❌ Agregar campos a `User` sin actualizar el schema

Si agregas un campo nuevo a `domain.User` y a la DB pero **olvidas** `user_schema.go`:
- La columna no aparece en `properties` de `SearchUsers`.
- Si la agregas como `Selectable: true` sin pensar si es sensible → leak.

**Correcto:** cada vez que agregas una columna, pregúntate: ¿es filtrable? ¿sortable? ¿selectable? ¿es sensible?

### ❌ Construir objetos concretos fuera de `main.go`

```go
// internal/core/service/user_service.go
func (s *UserService) Do() {
    rdb := redis.NewClient(...)   // ← NO, construir dentro del core
}
```

**Por qué mal:** rompe DIP. El service ya no recibe lo que necesita — lo fabrica.

**Correcto:** todo se construye en `main.go` y se inyecta.

### ❌ Devolver `pgx.ErrNoRows` hacia afuera del repo

```go
// MAL
func (r *UserRepo) FindByID(ctx, id) (*User, error) {
    err := row.Scan(...)
    return &u, err   // ← puede devolver pgx.ErrNoRows
}
```

**Por qué mal:** el core pasaría a depender de pgx (importaría `pgx.ErrNoRows` para compararlo).

**Correcto:** el repo traduce a `domain.ErrUserNotFound`:
```go
if errors.Is(err, pgx.ErrNoRows) {
    return nil, domain.ErrUserNotFound
}
```

### ❌ Copiar código entre repos

Si escribes `Search(...)` a mano en `SchoolRepo` cuando ya existe `SearchEngine`:
- Duplicas 30 líneas.
- Si un día hay un bug, arreglas uno y no el otro.

**Correcto:** embed `*SearchEngine`. Una línea.

---

## Checklist antes de mergear

Antes de hacer `git commit`, ejecuta mentalmente:

### 🔒 SOLID / hexagonal
- [ ] El core **no** importa `pgx`, `redis`, `bcrypt`, `grpc`.
  ```bash
  grep -rE "pgx|redis|bcrypt|grpc" internal/core/ || echo "✅ clean"
  ```
- [ ] Los repos nuevos tienen `var _ ports.XRepository = (*XRepo)(nil)`.
- [ ] Los services nuevos tienen `var _ ports.XUseCase = (*XService)(nil)`.
- [ ] No hay validación ni lógica de negocio en handlers.

### 🔐 Seguridad
- [ ] Campos sensibles (password_hash, token_hash, etc.) NO están en el proto.
- [ ] Campos sensibles NO están en el `*_schema.go`.
- [ ] Errores nuevos usan `apperr.NewX(...)`, no `errors.New(...)`.

### 🛠️ Infraestructura
- [ ] `go build ./...` sin errores.
- [ ] `go vet ./...` sin warnings.
- [ ] `buf generate` ejecutado si tocaste `.proto`.
- [ ] `go mod tidy` si agregaste deps.

### 📝 Consistencia
- [ ] Si agregaste un repo, agregaste wiring en `main.go`.
- [ ] Si el nuevo handler usa `UserUseCase`, el struct `UserService` sigue satisfaciendo la interfaz.
- [ ] Si agregaste columnas a una tabla, actualizaste el schema correspondiente.

### 🧪 Testing (recomendado)
- [ ] Tests unitarios del service (con mocks de los ports, sin DB real).
- [ ] Tests de integración del repo contra un Postgres real (testcontainers o docker-compose).

---

## Resumen en una tabla

| Quiero... | Archivos que toco | Tiempo |
|-----------|-------------------|--------|
| Agregar un error | `domain/errors.go` | 1 min |
| Agregar campo filtrable | `adapters/outbound/postgres/X_schema.go` | 1 min |
| Cambiar una regla de negocio | `core/domain/X.go` o `core/service/X_service.go` | 5 min |
| Agregar método a service existente | proto + ports + service + repo + handler | 20 min |
| Agregar búsqueda a entidad existente | schema + embed en repo + port + handler | 15 min |
| Cambiar Postgres por Mongo | `adapters/outbound/mongo/` + 3 líneas en main | 2 días |
| Agregar un CRUD completo (entidad nueva) | todo lo de R1 | ~2 horas |

**La ventaja de este patrón**: cualquier cosa que quieras hacer tiene un orden claro. Sigue la receta, respeta las reglas de oro, y el código queda consistente.

Si algo se siente complicado, probablemente estás violando una regla. Releela antes de seguir.

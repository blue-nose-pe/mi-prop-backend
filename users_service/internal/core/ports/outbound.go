package ports

import (
	"context"
	"time"

	"users_service/internal/core/domain"
	"users_service/internal/shared/search"
)

// -------------------------------------------------------------
// PUERTOS OUTBOUND (a.k.a. "driven ports"): lo que el CORE
// necesita del mundo externo. El core NO los implementa; los
// DECLARA. Los adapters outbound (Postgres, Redis, clientes
// gRPC hacia otros servicios...) los IMPLEMENTAN.
//
// Nombre alternativo: "driven" porque estos adapters son
// CONDUCIDOS por el core (el core los invoca). Visto desde el
// core: llamadas SALIENTES.
// -------------------------------------------------------------

// ---------- User (interfaces segregadas por responsabilidad) ----------
//
// UserReader, UserWriter y UserSearcher existen para que cualquier
// cliente (incluyendo tests y futuros servicios read-only) pueda
// depender SOLO de lo que usa. El composite UserRepository sigue
// disponible para el UserService actual, que legitimamente usa todo.

type UserReader interface {
	FindByID(ctx context.Context, id domain.UserID) (*domain.User, error)
	FindByEmail(ctx context.Context, email domain.Email) (*domain.User, error)
	ExistsByDocument(ctx context.Context, document string) (bool, error)
}

type UserWriter interface {
	Save(ctx context.Context, u *domain.User) (domain.UserID, error)
	Update(ctx context.Context, u *domain.User) error
	SetActive(ctx context.Context, id domain.UserID, active bool) error
	TouchLastAccess(ctx context.Context, id domain.UserID) error
	SetHubspotRecordID(ctx context.Context, id domain.UserID, recordID string) error

	// SetPassword: el dueño cambia su password (limpia must_change_password).
	SetPassword(ctx context.Context, id domain.UserID, newHash string) error
	// ResetPassword: un superadmin reset la password (mantiene must_change_password=1).
	ResetPassword(ctx context.Context, id domain.UserID, newHash string) error
	// SaveSuperadmin: insertar un user con is_superadmin=1 y must_change_password=1.
	// Lo usa el binario cmd/bootstrap.
	SaveSuperadmin(ctx context.Context, email, passwordHash string) (domain.UserID, error)
	// ExistsAnySuperadmin: para el bootstrap idempotente.
	ExistsAnySuperadmin(ctx context.Context) (bool, error)
}

type UserSearcher interface {
	Search(ctx context.Context, req search.Request) (*search.Response, error)
}

// UserRepository: composite para quien necesite las tres.
// Un adapter (ej: UserRepo de postgres) satisface esta automaticamente
// por tener todos los metodos.
type UserRepository interface {
	UserReader
	UserWriter
	UserSearcher
}

// ---------- Permissions ----------

type PermissionRepository interface {
	FindCodesByUserID(ctx context.Context, userID domain.UserID) ([]string, error)
	GroupExists(ctx context.Context, groupID uint32) (bool, error)
	AssignGroupToUser(ctx context.Context, userID domain.UserID, groupID uint32) error
	RevokeGroupFromUser(ctx context.Context, userID domain.UserID, groupID uint32) error

	// CRUD de grupos.
	CreateGroup(ctx context.Context, code, name, description string, permissionIDs []uint32) (uint32, error)
	UpdateGroup(ctx context.Context, id uint32, name, description string) error
	DeleteGroup(ctx context.Context, id uint32) error
	FindGroupByID(ctx context.Context, id uint32) (*domain.PermissionGroup, error)
	ListGroups(ctx context.Context) ([]domain.PermissionGroup, error)

	// Permisos por grupo.
	AddPermissionToGroup(ctx context.Context, groupID, permissionID uint32) error
	RemovePermissionFromGroup(ctx context.Context, groupID, permissionID uint32) error

	// Catálogo de permisos.
	ListPermissions(ctx context.Context) ([]domain.Permission, error)
	PermissionExists(ctx context.Context, permissionID uint32) (bool, error)
	GroupHasUsers(ctx context.Context, groupID uint32) (bool, error)

	// Users dentro de un grupo, paginado y con search por email/first_name/last_name/document_number.
	ListUsersInGroup(ctx context.Context, in ListUsersInGroupInput) ([]domain.User, uint32, error)
}

// ListUsersInGroupInput: filtros del listado de users por grupo.
type ListUsersInGroupInput struct {
	GroupID    uint32
	Search     string // matchea email, first_name, last_name, document_number (LIKE %s%)
	Limit      uint32 // default 100, máx 1000
	Offset     uint32
	ActiveOnly bool
}

// ---------- School ----------

type SchoolRepository interface {
	FindByID(ctx context.Context, id domain.SchoolID) (*domain.School, error)
	List(ctx context.Context, in ListSchoolsInput) ([]domain.School, uint32, error)
	SetHubspotRecordID(ctx context.Context, id domain.SchoolID, recordID string) error
	Create(ctx context.Context, s *domain.School) (domain.SchoolID, error)
	Update(ctx context.Context, s *domain.School) error
}

// ListSchoolsInput: filtros del listado paginado de colegios.
type ListSchoolsInput struct {
	Search     string // filtro por nombre (LIKE %search%)
	Limit      uint32
	Offset     uint32
	ActiveOnly bool
}

// ---------- Cache ----------

type UserCache interface {
	Set(ctx context.Context, u *domain.User) error
	Get(ctx context.Context, id domain.UserID) (*domain.User, error)
	Delete(ctx context.Context, id domain.UserID) error
}

// ---------- Password Hashing ----------

type PasswordHasher interface {
	Hash(plain string) (string, error)
	Compare(hashed, plain string) error
}

// ---------- JWT ----------
//
// El core NO conoce HS256, claims, ni TTLs concretos — eso vive en el
// adapter (internal/shared/jwtmw). El core solo usa estas dos primitivas:
// emitir un par access+refresh y validar un token de refresh para rotar.

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	RefreshJTI   string    // ID del refresh token, para persistir en BD
	AccessExp    time.Time // expiración del access (UTC)
	RefreshExp   time.Time // expiración del refresh (UTC)
}

type TokenIssueParams struct {
	UserID      domain.UserID
	Email       string
	Roles       []string
	Permissions []string
	SchoolID    string
}

type TokenClaims struct {
	UserID    domain.UserID
	JTI       string
	Type      string // "access" | "refresh"
	ExpiresAt time.Time
}

type TokenIssuer interface {
	IssuePair(p TokenIssueParams) (*TokenPair, error)
}

type TokenVerifier interface {
	Verify(token string) (*TokenClaims, error) // valida firma + exp + issuer
}

// ---------- Refresh Token Storage ----------
//
// Los refresh tokens se persisten para permitir revocación (Logout) y
// rotación segura (al hacer Refresh, el viejo se marca replaced_by).

type RefreshTokenRecord struct {
	JTI         string
	UserID      domain.UserID
	IssuedAt    time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time
	ReplacedBy  string
	IP          string
	UserAgent   string
}

type RefreshTokenRepository interface {
	Save(ctx context.Context, r *RefreshTokenRecord) error
	FindByJTI(ctx context.Context, jti string) (*RefreshTokenRecord, error)
	Revoke(ctx context.Context, jti string, replacedBy string) error
	RevokeAllForUser(ctx context.Context, userID domain.UserID) error
}

// ---------- Audit Log ----------
//
// El interceptor auditmw escribe a un Sink. Definimos el contrato acá
// porque el core podría querer auditar también sin pasar por gRPC
// (ej. tareas batch).

type AuditEvent struct {
	ActorUserID   domain.UserID
	Action        string
	EntityType    string
	EntityID      string
	PayloadJSON   string
	CorrelationID string
	IP            string
	Success       bool
	ErrorMessage  string
	CreatedAt     time.Time
}

type AuditSink interface {
	Record(ctx context.Context, ev AuditEvent) error
}

// ---------- Assignment (histórico de reasignaciones) ----------

type AssignmentKind string

const (
	AssignmentAsesorDeColegio       AssignmentKind = "asesor_de_colegio"
	AssignmentCoordinadorDeColegio  AssignmentKind = "coordinador_de_colegio"
	AssignmentEstudianteEnColegio   AssignmentKind = "estudiante_en_colegio"
	AssignmentAsesorDeEstudiante    AssignmentKind = "asesor_de_estudiante"
)

type AssignmentRecord struct {
	ID           string
	Kind         AssignmentKind
	SourceUserID domain.UserID
	TargetUserID domain.UserID
	ValidFrom    time.Time
	ValidTo      *time.Time // NULL = vigente
	CreatedBy    domain.UserID
	CreatedAt    time.Time
}

type AssignmentRepository interface {
	// Reassign cierra la asignación vigente (si existe) e inserta una nueva,
	// todo en una sola transacción. Preserva histórico.
	Reassign(ctx context.Context, kind AssignmentKind, source, target, by domain.UserID) error
	// FindCurrent retorna la asignación vigente para un (kind, target).
	FindCurrent(ctx context.Context, kind AssignmentKind, target domain.UserID) (*AssignmentRecord, error)
	// ListHistory retorna todas las asignaciones (vigentes e históricas)
	// para un (kind, target), ordenadas por valid_from DESC.
	ListHistory(ctx context.Context, kind AssignmentKind, target domain.UserID) ([]AssignmentRecord, error)
}

// ---------- OTP (login estudiante) ----------

type OTPRepository interface {
	// Save inserta un nuevo OTP. El hash y la expiración ya vienen calculados.
	Save(ctx context.Context, t *domain.OTPToken) error
	// FindActiveByUser retorna el OTP activo más reciente del user
	// (no consumido y no vencido). Devuelve ErrOTPInvalid si no hay.
	FindActiveByUser(ctx context.Context, userID domain.UserID) (*domain.OTPToken, error)
	// Consume marca consumed_at = now (idempotente).
	Consume(ctx context.Context, id string) error
	// IncrementAttempts suma 1 a attempts. Si attempts >= max, también consume.
	IncrementAttempts(ctx context.Context, id string) error
	// InvalidateAllForUser marca consumed_at todos los OTP activos del user.
	// Lo llama Request al pedir uno nuevo (solo 1 OTP activo por user).
	InvalidateAllForUser(ctx context.Context, userID domain.UserID) error
}

// OTPHasher: para 6 dígitos numéricos NO usamos bcrypt (overkill + costo).
// Implementación: HMAC-SHA256(secret, code) → hex.
type OTPHasher interface {
	Hash(plain string) string
	Compare(hashed, plain string) bool
}

// OTPSender: envía el OTP al user (típicamente vía HubSpot webhook → email).
// La implementación concreta vive en internal/adapters/outbound/otpsender
// y dialea hubspot_service.HubspotService/SendOTP.
type OTPSender interface {
	Send(ctx context.Context, email, plainOTP string) error
}

// StudentClassifier: decide si un user loguea via OTP (estudiante) o via
// email+password (admin/asesor/coordinador). En el modelo actual un user es
// "estudiante" si pertenece al permission_group "student_permissions".
type StudentClassifier interface {
	IsStudent(ctx context.Context, userID domain.UserID) (bool, error)
}

// HubspotContact: payload de sync al CRM. Campos opcionales vacíos.
type HubspotContact struct {
	UserID    domain.UserID
	Email     string
	DNI       string
	FirstName string
	LastName  string
}

// HubspotSyncer: upsert de contact al CRM. Best-effort, invocado en
// goroutine separada por el caller.
type HubspotSyncer interface {
	UpsertContact(ctx context.Context, c HubspotContact) error
}

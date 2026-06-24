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
	// FindGroupByCode resuelve un grupo por su `code` estable (ej.
	// "student_permissions"). Lo usa el flujo de auto-registro publico
	// de estudiantes para asignar el grupo correcto sin hard-codear ID.
	FindGroupByCode(ctx context.Context, code string) (*domain.PermissionGroup, error)
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
	// ListByAsesor devuelve los colegios donde el user dado es el asesor
	// vigente (assignment kind=asesor_de_colegio, valid_to IS NULL). Una
	// sola query con JOIN para evitar round-trips entre analytics y users.
	ListByAsesor(ctx context.Context, asesorID domain.UserID) ([]domain.School, error)
	// ListCoordinadores devuelve los usuarios coordinadores VIGENTES del colegio
	// (assignment kind=coordinador_de_colegio, target=school.user_id). Many-to-many.
	ListCoordinadores(ctx context.Context, schoolID domain.SchoolID) ([]domain.User, error)
}

// ListSchoolsInput: filtros del listado paginado de colegios.
type ListSchoolsInput struct {
	Search     string // filtro por nombre (LIKE %search%)
	Limit      uint32
	Offset     uint32
	ActiveOnly bool
}

// ---------- Lead (landing publica "Preparate") ----------

type LeadRepository interface {
	// Create persiste un lead nuevo (modelo APPEND, no upsert) y devuelve
	// su UUID generado por la BD.
	Create(ctx context.Context, l *domain.Lead) (domain.LeadID, error)
	// List devuelve leads paginados, mas reciente primero, opcionalmente
	// filtrando por texto (nombre/dni/correo) y/o key_code. Devuelve tambien
	// el total sin paginar para la reporteria del simulacro masivo.
	List(ctx context.Context, in ListLeadsInput) ([]domain.Lead, uint32, error)
	// FindByID recupera un lead por su UUID.
	FindByID(ctx context.Context, id domain.LeadID) (*domain.Lead, error)
	// ListByIDs recupera varios leads por sus UUIDs (envio masivo del panel).
	ListByIDs(ctx context.Context, ids []domain.LeadID) ([]domain.Lead, error)
	// MarkAccessSent marca que al lead se le envio el correo de acceso con la
	// key indicada (anti-doble-envio + estado en el panel).
	MarkAccessSent(ctx context.Context, id domain.LeadID, keyCode string) error
	// SetHubspotRecordID guarda el record_id que devuelve HubSpot tras el
	// upsert del contacto del lead.
	SetHubspotRecordID(ctx context.Context, id domain.LeadID, recordID string) error
}

// ListLeadsInput: filtros del listado paginado de leads del masivo.
type ListLeadsInput struct {
	Search  string // matchea first_name/last_name/dni/email (LIKE %search%)
	KeyCode string // filtro por campaña (key masiva), opcional
	Limit   uint32
	Offset  uint32
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
	UserID             domain.UserID
	Email              string
	Roles              []string
	Permissions        []string
	SchoolID           string
	MustChangePassword bool
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

	// ---- Asignaciones MUCHOS-a-muchos (varios source por target) ----
	// A diferencia de Reassign (que cierra TODA vigente del target = 1 por
	// colegio), estos operan por PAR (source, target): permiten varios source
	// vigentes para un mismo target. Se usan para "varios coordinadores por
	// colegio".
	//
	// AddSource cierra la vigente del par exacto (idempotente, evita duplicados)
	// e inserta una nueva. RevokeSource cierra la vigente del par.
	AddSource(ctx context.Context, kind AssignmentKind, source, target, by domain.UserID) error
	RevokeSource(ctx context.Context, kind AssignmentKind, source, target, by domain.UserID) error
	// ListSourcesByTarget retorna los source_user_id vigentes para (kind, target).
	ListSourcesByTarget(ctx context.Context, kind AssignmentKind, target domain.UserID) ([]domain.UserID, error)
}

// ---------- Visita ----------

type VisitaRepository interface {
	Create(ctx context.Context, v *domain.Visita) (domain.VisitaID, error)
	Update(ctx context.Context, v *domain.Visita) error
	FindByID(ctx context.Context, id domain.VisitaID) (*domain.Visita, error)
	List(ctx context.Context, in ListVisitasInput) ([]domain.Visita, uint32, error)
	CountByAsesorStatus(ctx context.Context, asesorID domain.UserID, status domain.VisitaStatus) (int32, error)
}

type ListVisitasInput struct {
	AsesorUserID domain.UserID
	SchoolID     domain.SchoolID
	Status       domain.VisitaStatus
	From         *time.Time // scheduled_at >= From (si !nil)
	To           *time.Time // scheduled_at <  To   (si !nil)
	Limit        uint32
	Offset       uint32
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
//
// El metodo "Send" minimal se mantiene por compatibilidad con tests y el
// noop sender. SendWithContact se usa desde sendStudentOTP cuando
// tenemos los datos del estudiante (registro o reactivacion). El adapter
// real los forwardea al proto SendOTPRequest para que hubspot_service
// upsertee el contacto con nombre/apellido/dni/phone, evitando que
// quede vacio en HubSpot.
type OTPSender interface {
	Send(ctx context.Context, email, plainOTP string) error
	SendWithContact(ctx context.Context, in OTPSendInput) error
}

// OTPSendInput — payload que sendStudentOTP arma con los datos del user
// recien creado/encontrado. Email/PlainOTP son obligatorios; el resto se
// envia cuando esta disponible.
type OTPSendInput struct {
	Email          string
	PlainOTP       string
	FirstName      string
	LastName       string
	DocumentNumber string
	Phone          string
	SchoolID       string // UUID del colegio (uso interno, no se manda a HubSpot)
	SchoolIntID    int32  // INT IDENTITY de la tabla school; se mapea a
	                       // mi_proposito___id_colegio (INTEGER en HubSpot)
	SchoolRecordID string // school.hubspot_record_id; el hubspot_service lo
	                       // usa para asociar Contact <-> Company despues
	                       // del upsert del estudiante.
	// MASIVO (landing "Preparate"): cuando WantMagicLink=true y hay
	// FRONT_BASE_URL configurado, sendStudentOTPFull arma MagicLinkURL =
	// <front>/test/simulacro/ingresar?e=<email>&c=<otp> y el correo incluye un
	// boton "Ingresar a mi examen". Asi el alumno entra DESDE el correo (fiel
	// a prod) sin tener que escribir el codigo. Solo lo activa el registro
	// masivo (key LAN, sin colegio) — el login normal por OTP no lo usa.
	WantMagicLink bool
	MagicLinkURL  string
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

package ports

import (
	"context"

	"users_service/internal/core/domain"
	"users_service/internal/shared/search"
)

// -------------------------------------------------------------
// PUERTOS INBOUND (a.k.a. "driving ports"): lo que el MUNDO
// EXTERIOR puede pedirle al CORE. Los adapters inbound (gRPC
// handler, CLI, consumers de eventos...) DEPENDEN de estas
// interfaces. Cambiar de transporte NO toca el core.
//
// Separación CQRS:
//   - *Commands  → mutan estado (Create, Update, Delete, Login...).
//   - *Queries   → solo leen, idempotentes, sin efectos secundarios.
//
// Se agrupan por "facet" (entidad o área temática), no método-por-método,
// para evitar explosión de interfaces. Cada facet es lo bastante chica
// (3-5 métodos) para no violar Interface Segregation.
// -------------------------------------------------------------

// =============== USER ===============

type UserCommands interface {
	Create(ctx context.Context, in CreateUserInput) (*domain.User, error)
	Update(ctx context.Context, in UpdateUserInput) (*domain.User, error)
	Deactivate(ctx context.Context, id domain.UserID) error
	Reactivate(ctx context.Context, id domain.UserID) (*domain.User, error)
	// ChangePassword: el dueño cambia su propia password verificando la
	// vieja. Limpia el flag must_change_password.
	ChangePassword(ctx context.Context, in ChangePasswordInput) error
	// ResetPassword: solo superadmin. Genera password aleatoria server-side,
	// la persiste hasheada con must_change_password=true, y devuelve la
	// temporal en claro (UNA SOLA VEZ — el admin la entrega al user).
	ResetPassword(ctx context.Context, in ResetPasswordInput) (*ResetPasswordOutput, error)
}

type UserQueries interface {
	Get(ctx context.Context, id domain.UserID) (*domain.User, error)
	GetByEmail(ctx context.Context, email domain.Email) (*domain.User, error)
	Search(ctx context.Context, req search.Request) (*search.Response, error)
	// Me: equivalente a Get pero también carga las permissions efectivas.
	// Lo consume /api/users/me para que el front sepa qué UI habilitar.
	Me(ctx context.Context, id domain.UserID) (*MeOutput, error)
}

// MeOutput agrega user + permissions + flags útiles para el front.
type MeOutput struct {
	User        *domain.User
	Permissions []string // codes en formato `<service>.<table>.<action>`
}

// =============== AUTH ===============

// AuthCommands agrupa autenticación. Authenticate verifica credenciales
// (sin emitir tokens — útil para integraciones internas). Login emite
// access+refresh JWT. Refresh rota el refresh token. Logout lo revoca.
type AuthCommands interface {
	Authenticate(ctx context.Context, in AuthenticateInput) (*AuthenticateOutput, error)
	Login(ctx context.Context, in LoginInput) (*LoginOutput, error)
	Refresh(ctx context.Context, in RefreshInput) (*RefreshOutput, error)
	Logout(ctx context.Context, in LogoutInput) error
	// RequestStudentOTP: genera y envía un OTP al email del estudiante.
	// Comportamiento "silencioso" para evitar enumeration: si el email no
	// existe o el user no es estudiante, devuelve OK sin enviar nada.
	RequestStudentOTP(ctx context.Context, in RequestStudentOTPInput) error
	// VerifyStudentOTP: valida el OTP y emite par access+refresh JWT.
	VerifyStudentOTP(ctx context.Context, in VerifyStudentOTPInput) (*LoginOutput, error)
}

// =============== PERMISSION ===============

type PermissionCommands interface {
	AssignGroup(ctx context.Context, userID domain.UserID, groupID uint32) error
	RevokeGroup(ctx context.Context, userID domain.UserID, groupID uint32) error
}

type PermissionQueries interface {
	ListUserPermissions(ctx context.Context, userID domain.UserID) ([]string, error)
	HasPermission(ctx context.Context, userID domain.UserID, code string) (bool, error)
}

// =============== PERMISSION GROUP (administración de roles) ===============
//
// El cliente combina permisos individuales (catálogo `permission`) en
// grupos reutilizables. Los grupos se asignan luego a users via
// PermissionCommands.AssignGroup. El catálogo de permisos atómicos lo
// gobierna Blue Nose vía migraciones SQL — el cliente solo lo lista.

type PermissionGroupCommands interface {
	Create(ctx context.Context, in CreateGroupInput) (*domain.PermissionGroup, error)
	Update(ctx context.Context, in UpdateGroupInput) (*domain.PermissionGroup, error)
	Delete(ctx context.Context, id uint32) error
	AddPermission(ctx context.Context, groupID, permissionID uint32) error
	RemovePermission(ctx context.Context, groupID, permissionID uint32) error
}

type PermissionGroupQueries interface {
	Get(ctx context.Context, id uint32) (*domain.PermissionGroup, error)
	List(ctx context.Context) ([]domain.PermissionGroup, error)
	ListPermissions(ctx context.Context) ([]domain.Permission, error)
}

// CreateGroupInput / UpdateGroupInput: los DTOs viajan por el contrato
// del core, independientes del transporte. PermissionIDs en Create es
// opcional — el cliente puede agregarlos después con AddPermission.
type CreateGroupInput struct {
	Code          string
	Name          string
	Description   string
	PermissionIDs []uint32
}

type UpdateGroupInput struct {
	ID          uint32
	Name        string
	Description string
}

// =============== HUBSPOT SYNC ===============

// HubspotSyncCommands lo consume el hubspot-service vía gRPC: cuando
// crea/actualiza el contact (o el custom object de school) en HubSpot,
// devuelve aquí el record_id y users_service lo persiste para que los
// updates posteriores no necesiten search-by-DNI/email.
type HubspotSyncCommands interface {
	SetUserHubspotRecordID(ctx context.Context, userID domain.UserID, recordID string) error
	SetSchoolHubspotRecordID(ctx context.Context, schoolID domain.SchoolID, recordID string) error
}

// =============== ASSIGNMENT (histórico de reasignaciones) ===============

type AssignmentCommands interface {
	// Reassign cierra la asignación vigente (si la hay) y crea una nueva.
	// Si Source es vacío, solo cierra la vigente (desasignar).
	Reassign(ctx context.Context, in ReassignInput) error
}

type AssignmentQueries interface {
	GetCurrent(ctx context.Context, in GetCurrentAssignmentInput) (*AssignmentSnapshot, error)
	ListHistory(ctx context.Context, in ListAssignmentHistoryInput) ([]AssignmentSnapshot, error)
}

type ReassignInput struct {
	Kind   AssignmentKind
	Source domain.UserID // "" → desasignar
	Target domain.UserID
	By     domain.UserID // quién hizo el cambio
}

type GetCurrentAssignmentInput struct {
	Kind   AssignmentKind
	Target domain.UserID
}

type ListAssignmentHistoryInput struct {
	Kind   AssignmentKind
	Target domain.UserID
}

// AssignmentSnapshot es la vista que el core devuelve al exterior
// (alias del record outbound; mismo paquete `ports`, no exponemos
// estructuras nuevas para evitar doble mapping).
type AssignmentSnapshot = AssignmentRecord

// =============== DTOs de entrada/salida ===============
// Viven aquí porque pertenecen al contrato del core, no a un transporte.

type CreateUserInput struct {
	Email          string
	Password       string // texto plano; el core hashea via PasswordHasher
	FirstName      string
	LastName       string
	DocumentNumber string
	SchoolID       string // "" = sin colegio
}

type UpdateUserInput struct {
	ID             domain.UserID
	FirstName      string
	LastName       string
	DocumentNumber string
	SchoolID       string // "" = no tocar
}

// ChangePasswordInput: el dueño cambia su propia password.
// El handler valida OldPassword contra el hash actual antes de aplicar
// el cambio (anti tomar-cuenta-de-otro-en-misma-PC).
type ChangePasswordInput struct {
	UserID      domain.UserID
	OldPassword string
	NewPassword string
}

// ResetPasswordInput: SOLO superadmin. Reset desde panel admin.
// Genera password aleatoria server-side (no la elige el caller) y
// devuelve la temporal en el output para que el admin se la entregue
// al user. Marca must_change_password=true.
type ResetPasswordInput struct {
	ActorIsSuperadmin bool          // setea el handler gRPC desde claims JWT
	TargetUserID      domain.UserID
}

// ResetPasswordOutput: la password temporal viaja UNA SOLA VEZ por la
// respuesta gRPC; el server NO la persiste en claro y el admin debe
// entregársela al user inmediatamente. El user verá must_change_password=true
// en su próximo login y será forzado a cambiarla.
type ResetPasswordOutput struct {
	TempPassword string
}

type AuthenticateInput struct {
	Email         domain.Email
	PlainPassword string
}

type AuthenticateOutput struct {
	User        *domain.User
	Permissions []string
}

// LoginInput / LoginOutput: emiten un par access+refresh JWT. La IP y
// el user-agent se usan para auditar el origen de la sesión.
type LoginInput struct {
	Email         domain.Email
	PlainPassword string
	IP            string
	UserAgent     string
}

type LoginOutput struct {
	User         *domain.User
	Permissions  []string
	AccessToken  string
	RefreshToken string
}

// RefreshInput / RefreshOutput: validan el refresh token, lo rotan
// (revocan el viejo, emiten uno nuevo) y devuelven un nuevo access.
type RefreshInput struct {
	RefreshToken string
	IP           string
	UserAgent    string
}

type RefreshOutput struct {
	AccessToken  string
	RefreshToken string // refresh nuevo (rotación)
}

// LogoutInput revoca el refresh token. El access token sigue siendo
// criptográficamente válido hasta su exp natural (15 min) — el cliente
// debe descartarlo.
type LogoutInput struct {
	RefreshToken string
}

// RequestStudentOTPInput: dispara el envío de un OTP de 6 dígitos al
// email del estudiante. Si el email no existe o el user no es estudiante,
// el comando devuelve OK sin enviar nada (anti enumeration attack).
type RequestStudentOTPInput struct {
	Email domain.Email
	IP    string
}

// VerifyStudentOTPInput: valida el OTP recibido y, si es correcto, emite
// un par access+refresh igual que Login normal.
type VerifyStudentOTPInput struct {
	Email     domain.Email
	OTP       string // 6 dígitos
	IP        string
	UserAgent string
}

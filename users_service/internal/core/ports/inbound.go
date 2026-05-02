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
}

type UserQueries interface {
	Get(ctx context.Context, id domain.UserID) (*domain.User, error)
	GetByEmail(ctx context.Context, email domain.Email) (*domain.User, error)
	Search(ctx context.Context, req search.Request) (*search.Response, error)
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

// =============== HUBSPOT SYNC (resuelve los 3 TODOs del P1) ===============

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

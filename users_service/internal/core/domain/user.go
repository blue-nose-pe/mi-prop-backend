package domain

import (
	"regexp"
	"strings"
	"time"
)

// UserID y SchoolID son UUIDs generados server-side por SQL Server
// (UNIQUEIDENTIFIER con DEFAULT NEWID()). Los transportamos como string.
type UserID string
type SchoolID string
type Email string

// User es la entidad raíz. Cero imports de infraestructura acá.
type User struct {
	ID              UserID
	Email           Email
	PasswordHash    string
	FirstName       string
	LastName        string
	DocumentNumber  string
	Phone           string   // "" = no se cargo; se sincroniza a HubSpot
	SchoolID        SchoolID // "" = no pertenece a un colegio
	Active          bool
	LastAccessAt    *time.Time
	CreatedAt       time.Time
	UpdatedAt       *time.Time
	HubspotRecordID string // "" = aún no sincronizado con HubSpot

	// IsSuperadmin BYPASSA el chequeo de permisos. Solo para los users
	// que el bootstrap k8s Job o un superadmin ya existente promueven
	// explícitamente. Por convención: solo un superadmin puede editar
	// el catálogo `permission` y crear otros superadmins.
	IsSuperadmin bool

	// MustChangePassword fuerza al user a cambiar la password en su
	// primer login (true después del bootstrap o cuando un admin reseta
	// la password). Mientras esté true, todas las rutas excepto
	// /api/users/me y /api/users/me/change-password devuelven 403.
	MustChangePassword bool

	// IntID — INT IDENTITY (migration 021). Equivalente v2 al ID MySQL int
	// que v1 usaba. Lo usa la integracion con HubSpot — la prop custom
	// mi_proposito_asesor_id del custom object Asesor esta tipada como
	// INTEGER y el UUID de v2 no es parseable.
	IntID int32

	// UserType — derivado del permission_group del user (no se persiste
	// como columna, se computa al leer). Valores:
	//   1 admin | 2 asesor | 3 coordinador | 4 student | 0 sin grupo
	// El front v1 lo usa para rutear al dashboard correcto post-login;
	// sin esto, todos los users no-superadmin caen al fallback /student/dashboard.
	UserType int32
}

// UserTypeFromGroupCode mapea el code estable del permission_group al
// numero que espera el front v1 (ucsp-front routing post-login). Si el
// user pertenece a varios grupos, el caller debe elegir el de menor
// valor (admin > asesor > coordinador > student).
func UserTypeFromGroupCode(code string) int32 {
	switch code {
	case "admin_permissions":
		return 1
	case "asesor_permissions":
		return 2
	case "coordinador_permissions":
		return 3
	case "student_permissions":
		return 4
	}
	return 0
}

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

// ValidatePasswordStrength: regla de negocio pura (vive en el core,
// no en el adapter de transporte).
func ValidatePasswordStrength(plain string) error {
	if len(plain) < 8 {
		return ErrWeakPassword
	}
	return nil
}

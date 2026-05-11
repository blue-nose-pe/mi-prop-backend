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

package domain

import (
	"regexp"
	"strings"
	"time"
)

// Value Objects tipados. UserID y SchoolID son UUIDs (v7) generados
// por Postgres. Los transportamos como string por simplicidad — si
// manana se necesita validar formato, se agrega un metodo Validate().
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
	SchoolID        SchoolID // "" = no pertenece a un colegio
	Active          bool
	LastAccessAt    *time.Time
	CreatedAt       time.Time
	UpdatedAt       *time.Time
	HubspotRecordID string // "" = aún no sincronizado con HubSpot
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

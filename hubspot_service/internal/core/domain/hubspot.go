// Package domain — entidades del dominio hubspot_service.
//
// Cero imports de infraestructura. Las entidades modelan lo que existe
// EN HubSpot (Contact, custom objects Key/Asesor/Colegio) más los
// "trabajos" que el servicio acepta (sync result, OTP, upserts).
package domain

import "time"

// Identificadores tipados.
type (
	UserID    string // ref lógica db_users.users.id
	SchoolID  string // ref lógica db_users.school.id
	AttemptID string // ref lógica exams_service.exam_attempt.id
	RecordID  string // record_id que devuelve HubSpot
)

// ExamTypeCode determina a qué propiedad del Contact va el score.
type ExamTypeCode string

const (
	ExamVocacional ExamTypeCode = "vocacional"
	ExamSimulacro  ExamTypeCode = "simulacro"
	ExamHabitos    ExamTypeCode = "habitos"
)

func (c ExamTypeCode) Valid() bool {
	switch c {
	case ExamVocacional, ExamSimulacro, ExamHabitos:
		return true
	}
	return false
}

// Contact es el subset de propiedades de un Contact que el servicio
// manipula. Los campos opcionales se omiten del payload si están vacíos
// (no se sobrescriben en HubSpot con strings vacíos).
type Contact struct {
	DNI         string
	Email       string
	FirstName   string
	LastName    string
	Phone       string
	SchoolName  string
	Grade       string
	Extra       map[string]string // propiedades adicionales no modeladas
	UserID      UserID            // si el cliente lo provee, persistimos record_id en users
}

// ToProperties devuelve el bag de propiedades para HubSpot, excluyendo
// strings vacíos.
func (c Contact) ToProperties() map[string]string {
	props := map[string]string{}
	if c.DNI != "" {
		props["dni"] = c.DNI
	}
	if c.Email != "" {
		props["email"] = c.Email
	}
	if c.FirstName != "" {
		props["firstname"] = c.FirstName
	}
	if c.LastName != "" {
		props["lastname"] = c.LastName
	}
	if c.Phone != "" {
		props["phone"] = c.Phone
	}
	if c.SchoolName != "" {
		props["colegio_actual"] = c.SchoolName
	}
	if c.Grade != "" {
		props["grado"] = c.Grade
	}
	for k, v := range c.Extra {
		if v != "" {
			props[k] = v
		}
	}
	return props
}

// ExamResult es el payload que exams_service envía al cerrar un attempt.
type ExamResult struct {
	DNI            string
	ExamTypeCode   ExamTypeCode
	Score          int32
	MaxScore       int32
	AttemptID      AttemptID
	SubmittedAt    time.Time
	ContactRecord  RecordID // opcional: si ya conocemos el record_id evita un search
}

// ToProperties mapea el resultado a las propiedades HubSpot que se
// escriben en el Contact (score_<modulo>, max_score_<modulo>, ultima_evaluacion).
func (r ExamResult) ToProperties() map[string]string {
	return map[string]string{
		"score_" + string(r.ExamTypeCode):     intStr(r.Score),
		"max_score_" + string(r.ExamTypeCode): intStr(r.MaxScore),
		"ultima_evaluacion":                   r.SubmittedAt.UTC().Format(time.RFC3339),
	}
}

// AsesorPayload — propiedades del custom object Asesor.
type AsesorPayload struct {
	Email        string
	Nombre       string
	Bio          string
	AsesorUserID UserID
}

// ColegioPayload — propiedades del custom object Colegio.
type ColegioPayload struct {
	Email          string
	Nombre         string
	PersonalACargo string
	SchoolID       SchoolID
}

// FailedSync — vista de un job que terminó en DLQ (admin lo consulta).
type FailedSync struct {
	Queue        string
	JobID        string
	FailedReason string
	Payload      string
	FailedAt     time.Time
}

func intStr(n int32) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	buf := [11]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = digits[n%10]
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

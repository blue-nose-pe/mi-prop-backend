// Package domain — entidades del dominio hubspot_service.
//
// Cero imports de infraestructura. Las entidades modelan lo que existe
// EN HubSpot (Contact, custom objects Key/Asesor/Colegio) más los
// "trabajos" que el servicio acepta (sync result, OTP, upserts).
package domain

import (
	"fmt"
	"time"
)

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

// KeyPayload — propiedades del custom object Key (2-32450705) que P1 ya
// sincronizaba en createKey.js / createVisita.js. v2 replica el mismo
// shape para mantener compatibilidad con los Workflows del portal UCSP
// que filtran keys por la prop `herramienta`.
type KeyPayload struct {
	Code           string       // key.code (unique para upsert por prop "codigo")
	ExamTypeCode   ExamTypeCode // vocacional | simulacro | habitos
	AsesorUserID   UserID
	SchoolID       SchoolID
	SchoolName     string // denormalizado
	Grade          string
	Section        string
	ValidFrom      time.Time
	ValidTo        time.Time
	MaxUses        int32
	// record_ids opcionales: si vienen seteados se asocia directo.
	AsesorRecordID RecordID
	SchoolRecordID RecordID
	// AsesorEmail opcional: si AsesorRecordID viene vacio + AsesorEmail
	// presente, hubspot-service busca el Asesor por email (prop unique).
	// Email matchea entre v1 y v2 (los asesores migrados conservan email).
	AsesorEmail string
}

// herramientaLabel mapea exam_type_code al string que P1 grababa en la
// prop `herramienta`. Cambiar estos labels rompe los Workflows del
// portal UCSP que filtran keys por herramienta.
func (k KeyPayload) HerramientaLabel() string {
	switch k.ExamTypeCode {
	case ExamVocacional:
		return "Test Vocacional"
	case ExamSimulacro:
		return "Test Examen Simulacro"
	case ExamHabitos:
		return "Test Estilos de Aprendizaje"
	}
	return ""
}

// ToProperties — bag de properties para el custom object Key.
//
// IMPORTANTE — props que P1 seteaba pero v2 NO setea por incompatibilidad
// de tipos en el portal UCSP (definidos cuando v1 usaba IDs MySQL int):
//   - colegio_id (INTEGER en HubSpot) — v2 SchoolID es UUID, no parseable
//   - asesor_id  (INTEGER en HubSpot) — v2 AsesorUserID es UUID, idem
//   - grado      (enum INTEGER [1,2,4]) — v2 Grade es string ("5to")
//   - seccion    (enum INTEGER [1-12])  — v2 Section es letra ("A")
// Mapear estos requiere lookup adicional / tabla de equivalencias. Por
// ahora se omiten silenciosamente: el record queda con codigo +
// herramienta + nombre_colegio + aforo + fechas, suficiente para que el
// CRM identifique la key. Las asociaciones a Asesor/Company (object
// links) se hacen aparte por record_id, no por estas props.
//
// Fechas: HubSpot espera DATE en milisegundos UTC fijados a medianoche
// (00:00:00). Truncamos cualquier hora del ValidFrom/ValidTo a date-only
// antes de serializar.
func (k KeyPayload) ToProperties() map[string]string {
	props := map[string]string{
		"codigo":      k.Code,
		"herramienta": k.HerramientaLabel(),
	}
	if k.SchoolName != "" {
		// P1 usaba nombre_colegio en simulacro/habitos y colegio_nombre en
		// vocacional; v2 normaliza a nombre_colegio.
		props["nombre_colegio"] = k.SchoolName
	}
	if !k.ValidFrom.IsZero() {
		props["desde"] = midnightUTCMillis(k.ValidFrom)
	}
	if !k.ValidTo.IsZero() {
		props["hasta"] = midnightUTCMillis(k.ValidTo)
	}
	if k.MaxUses > 0 {
		props["aforo"] = intStr(k.MaxUses)
	}
	return props
}

// midnightUTCMillis — HubSpot DATE props requieren milisegundos UTC al
// inicio del dia (00:00:00). Truncamos la hora antes de devolver el
// timestamp como string.
func midnightUTCMillis(t time.Time) string {
	utc := t.UTC()
	midnight := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	return fmt.Sprintf("%d", midnight.UnixMilli())
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

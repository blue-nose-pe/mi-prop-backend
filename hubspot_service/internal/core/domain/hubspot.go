// Package domain — entidades del dominio hubspot_service.
//
// Cero imports de infraestructura. Las entidades modelan lo que existe
// EN HubSpot (Contact, custom objects Key/Asesor/Colegio) más los
// "trabajos" que el servicio acepta (sync result, OTP, upserts).
package domain

import (
	"fmt"
	"strconv"
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
	Score          float64 // float: preserva decimales (Nacional 1.25/pregunta)
	MaxScore       float64
	AttemptID      AttemptID
	SubmittedAt    time.Time
	ContactRecord  RecordID // opcional: si ya conocemos el record_id evita un search
}

// ToProperties mapea el resultado a las propiedades HubSpot que se
// escriben en el Contact (score_<modulo>, max_score_<modulo>, ultima_evaluacion).
func (r ExamResult) ToProperties() map[string]string {
	return map[string]string{
		"score_" + string(r.ExamTypeCode):     FloatStr(r.Score),
		"max_score_" + string(r.ExamTypeCode): FloatStr(r.MaxScore),
		"ultima_evaluacion":                   r.SubmittedAt.UTC().Format(time.RFC3339),
	}
}

// FloatStr formatea un puntaje float con el mínimo de decimales necesarios
// (11.25 → "11.25", 100 → "100"). HubSpot acepta el string en props number.
func FloatStr(n float64) string {
	return strconv.FormatFloat(n, 'f', -1, 64)
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
	// SchoolIntID / AsesorIntID: equivalente v2 de los IDs MySQL int de v1.
	// SchoolIntID viene de school.int_id (migration 020). AsesorIntID viene
	// de users.int_id (migration que se agrega en cambio #4). 0 = no setear
	// la prop. Necesarios porque las props colegio_id/asesor_id del portal
	// UCSP son INTEGER y los UUIDs de v2 no son parseables.
	SchoolIntID int32
	AsesorIntID int32
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
// Props que se setean cuando hay datos parseables al formato esperado:
//   - codigo (string) — siempre
//   - herramienta (string) — derivado de exam_type_code
//   - nombre_colegio (string) — si SchoolName != ""
//   - aforo (int) — si MaxUses > 0
//   - desde/hasta (date, midnight UTC ms) — si las fechas no son zero
//   - seccion (enum INT [1-12]) — si Section es letra A..L (mapeo A=1..L=12)
//   - colegio_id (INT) — si SchoolIntID > 0 (school.int_id de v2)
//   - asesor_id (INT) — si AsesorIntID > 0 (user.int_id de v2 cuando exista)
//   - grado (enum INT [1,2,4]) — si Grade es "3"/"4"/"5" (mapeo 3ro=4, 4to=2, 5to=1)
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
	if n := seccionLetterToInt(k.Section); n > 0 {
		props["seccion"] = intStr(n)
	}
	if k.SchoolIntID > 0 {
		props["colegio_id"] = intStr(k.SchoolIntID)
	}
	if k.AsesorIntID > 0 {
		props["asesor_id"] = intStr(k.AsesorIntID)
	}
	if n := gradoToEnum(k.Grade); n > 0 {
		props["grado"] = intStr(n)
	}
	return props
}

// gradoToEnum mapea el grado escolar v2 (string "3"/"4"/"5") al enum INT
// que la prop `grado` del custom object Key acepta en el portal UCSP:
//   "5" → 1 (5to), "4" → 2 (4to), "3" → 4 (3ro). Valores definidos por
// HubSpot (GET /crm/v3/properties/2-32450705/grado). Cualquier otro
// input → 0 (skip silencioso, no se setea la prop — mismo patron que
// seccionLetterToInt).
func gradoToEnum(g string) int32 {
	switch g {
	case "5":
		return 1
	case "4":
		return 2
	case "3":
		return 4
	}
	return 0
}

// seccionLetterToInt mapea A..L → 1..12 (case-insensitive). v1 usaba
// numeros, v2 usa letras. HubSpot prop seccion es enum INT [1-12].
// Devuelve 0 si no es letra A-L (skip silencioso, no setea la prop).
func seccionLetterToInt(s string) int32 {
	if len(s) != 1 {
		return 0
	}
	c := s[0]
	if c >= 'a' && c <= 'z' {
		c -= 'a' - 'A'
	}
	if c < 'A' || c > 'L' {
		return 0
	}
	return int32(c - 'A' + 1)
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

package domain

import "time"

// LeadID es el UUID de un lead de la landing publica.
type LeadID string

// Lead representa una inscripcion en la landing publica "Preparate"
// (simulacro masivo). Es un contacto ANONIMO (sin cuenta de usuario): el
// alumno deja sus datos en la landing y luego rinde el simulacro UCSP con
// la key masiva. El lead viaja a HubSpot (portal UCSP) para que admision lo
// trabaje.
//
// Modelo APPEND, no upsert: un mismo DNI puede reinscribirse por campaña
// (fiel a prod). La deduplicacion es responsabilidad de la lectura.
type Lead struct {
	ID        LeadID
	FirstName string
	LastName  string
	DNI       string // "" = no registrado
	Phone     string // "" = no registrado
	Email     string
	// GraduationYear: anio de egreso del colegio (2015..2027). 0 = sin
	// especificar.
	GraduationYear int32
	// SchoolText: colegio de procedencia, texto libre que escribe el alumno.
	// "" = sin registrar.
	SchoolText string
	// Origen: en prod siempre "landing Simulacro".
	Origen string
	// KeyCode: codigo de la key masiva (LAN) de la campaña. "" = sin atar.
	KeyCode string
	// Consentimientos de la landing (Ley 29733).
	TermsAccepted  bool
	DataProcessing bool
	// HubspotRecordID: record_id del contacto en HubSpot. "" = aun no
	// sincronizado.
	HubspotRecordID string
	CreatedAt       time.Time
	// AccesoEnviadoAt: cuando se le envio el correo de acceso (magic-link) al
	// lead desde el panel masivo. nil = pendiente. Evita doble envio y permite
	// reenviar solo a los pendientes.
	AccesoEnviadoAt *time.Time
	// AccesoKeyCode: la key masiva (LAN) con la que se le envio el acceso.
	AccesoKeyCode string
}

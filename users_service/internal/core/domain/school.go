package domain

import "time"

// School representa un colegio. Cada colegio esta ligado a un usuario raiz
// (school.user_id). Otros usuarios pueden pertenecer al colegio via
// users.school_id.
type School struct {
	ID              SchoolID
	IntID           int32  // INT IDENTITY de BD. Lo usa la integracion con
	                        // HubSpot porque la prop custom mi_proposito___id_colegio
	                        // del portal UCSP esta tipada como INTEGER (heredado
	                        // de P1 que usaba MySQL autoincrement). NUNCA es PK
	                        // semantica — la PK sigue siendo el UUID en ID.
	UserID          UserID // usuario que representa al colegio
	Name            string
	City            string // ciudad/distrito del colegio. "" = sin definir.
	Category        string // Segmento UCSP: A1/A2/B1/B2/C/OP/OR (o legacy
	                        // A+/A/B/C/D para data pre-2026). "" si no
	                        // clasificado todavia.
	// Cliente: codigo modular MINEDU. Obligatorio y unico (constraint
	// ux_school_code filtered index, permite NULL durante backfill).
	Code            string
	// Cliente: nivel de penetracion del producto en el colegio. Editable
	// solo post-creacion. Valores: "Alta" / "Media" / "Baja" / "".
	Penetration     string
	// Cliente (2026-06-04): datos de contacto del colegio. Opcionales al
	// crear, se piden al editar. "" = sin registrar.
	Email           string
	Phone           string
	// Cliente (2026-06-04): RUC y poblacion estudiantil del colegio. Se
	// piden al editar. "" = sin registrar.
	Ruc             string
	Poblacion       string
	// Cliente (doc observaciones): persona/contacto a cargo del colegio
	// (campo de la vista original v1). Texto libre. "" = sin registrar.
	PersonalACargo  string
	// Solo lo llena el listado (List/ListByAsesor) via JOIN con assignment:
	// asesor vigente del colegio. "" = colegio SIN asesor. No persiste en
	// la tabla school (es derivado).
	AsesorUserID    string
	AsesorName      string
	// CoordinadoresNombres: nombres de los coordinadores vigentes del colegio,
	// separados por ", " (many-to-many vía assignment). Solo lectura (List).
	CoordinadoresNombres string
	Active               bool
	// ActiveRaw: solo escritura (Update). "" = no tocar; "true"/"false" =
	// activar/desactivar el colegio (C1). No se hidrata al leer (usar Active).
	ActiveRaw       string
	CreatedAt       time.Time
	UpdatedAt       *time.Time
	HubspotRecordID string // "" = aún no sincronizado con HubSpot
}

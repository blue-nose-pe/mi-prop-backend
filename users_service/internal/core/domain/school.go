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
	Category        string // tag UCSP: A+, A, B, C, D o "" si no clasificado.
	Active          bool
	CreatedAt       time.Time
	UpdatedAt       *time.Time
	HubspotRecordID string // "" = aún no sincronizado con HubSpot
}

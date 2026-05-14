package domain

import "time"

// School representa un colegio. Cada colegio esta ligado a un usuario raiz
// (school.user_id). Otros usuarios pueden pertenecer al colegio via
// users.school_id.
type School struct {
	ID              SchoolID
	UserID          UserID // usuario que representa al colegio
	Name            string
	City            string // ciudad/distrito del colegio. "" = sin definir.
	Category        string // tag UCSP: A+, A, B, C, D o "" si no clasificado.
	Active          bool
	CreatedAt       time.Time
	UpdatedAt       *time.Time
	HubspotRecordID string // "" = aún no sincronizado con HubSpot
}

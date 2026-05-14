package domain

import "time"

// VisitaID — value object para el id de visita.
type VisitaID string

// VisitaStatus enumera los estados de una visita. Cualquier valor fuera de
// esta lista es invalido; el CHECK constraint en la migration 019 los
// rechaza a nivel DB.
type VisitaStatus string

const (
	VisitaScheduled VisitaStatus = "scheduled"
	VisitaCompleted VisitaStatus = "completed"
	VisitaCancelled VisitaStatus = "cancelled"
	VisitaNoShow    VisitaStatus = "no_show"
)

// IsValid devuelve true si el valor es uno de los 4 estados aceptados.
func (s VisitaStatus) IsValid() bool {
	switch s {
	case VisitaScheduled, VisitaCompleted, VisitaCancelled, VisitaNoShow:
		return true
	}
	return false
}

// Visita representa el registro de una visita (presencial o remota) del
// asesor a un colegio.
type Visita struct {
	ID           VisitaID
	AsesorUserID UserID
	SchoolID     SchoolID
	ScheduledAt  time.Time
	CompletedAt  *time.Time   // NULL si todavia no se realizo
	Status       VisitaStatus // scheduled | completed | cancelled | no_show
	Notes        string       // "" si sin notas
	CreatedAt    time.Time
	UpdatedAt    *time.Time
}

package domain

import "time"

type (
	KeyID    string
	UserID   string // ref lógica a db_users.users.id
	SchoolID string // ref lógica a db_users.school.id
)

// KeyMode discrimina cómo se distribuye el código.
//   - school: atado a un colegio específico (los estudiantes lo usan al rendir).
//   - lan:    masivo (no atado a colegio); admin lo da en sesiones LAN.
type KeyMode string

const (
	ModeSchool KeyMode = "school"
	ModeLAN    KeyMode = "lan"
)

func (m KeyMode) Valid() bool { return m == ModeSchool || m == ModeLAN }

// Key — código de acceso a un test.
type Key struct {
	ID            KeyID
	Code          string
	ExamTypeID    int32
	SchoolID      SchoolID // "" si Mode == lan
	AsesorUserID  UserID
	Mode          KeyMode
	Grade         string
	Section       string
	ValidFrom     *time.Time
	ValidTo       *time.Time
	MaxUses       int32 // 0 = ilimitado
	CurrentUses   int32
	Active        bool
	CreatedAt     time.Time
	UpdatedAt     *time.Time
	// ExamID: si !="" la key sirve esa version puntual del exam (front
	// hace GET /api/exams/{exam_id}/questions directo). Si "" cae al
	// fallback legacy de buscar "primer exam publicado del exam_type_id".
	ExamID string
}

// IsUsable retorna true si la key está activa, dentro de su ventana
// temporal, y le quedan usos disponibles.
func (k Key) IsUsable(now time.Time) (bool, string) {
	if !k.Active {
		return false, "key is inactive"
	}
	if k.ValidFrom != nil && now.Before(*k.ValidFrom) {
		return false, "key not yet valid"
	}
	if k.ValidTo != nil && now.After(*k.ValidTo) {
		return false, "key expired"
	}
	if k.MaxUses > 0 && k.CurrentUses >= k.MaxUses {
		return false, "key reached max uses"
	}
	return true, ""
}

// KeyUsage es una entrada inmutable del audit de uso.
type KeyUsage struct {
	ID        string
	KeyID     KeyID
	UserID    UserID
	AttemptID string // "" si aún no se asoció attempt
	UsedAt    time.Time
}

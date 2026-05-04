package domain

import "time"

// OTPToken representa un código de un solo uso emitido para un user.
// El plaintext (6 dígitos) NUNCA se guarda — solo el hash.
type OTPToken struct {
	ID          string
	UserID      UserID
	CodeHash    string
	IssuedAt    time.Time
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
	Attempts    int32
	MaxAttempts int32
	IP          string
}

// IsActive: no consumido y no vencido.
func (t *OTPToken) IsActive(now time.Time) bool {
	return t.ConsumedAt == nil && t.ExpiresAt.After(now)
}

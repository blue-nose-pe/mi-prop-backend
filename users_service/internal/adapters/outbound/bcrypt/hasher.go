package bcryptadapter

import (
	"users_service/internal/core/ports"

	"golang.org/x/crypto/bcrypt"
)

// Hasher implementa ports.PasswordHasher usando bcrypt.
// El core pide "hashea esto"; no conoce bcrypt. Si cambiamos a argon2,
// solo creamos otro adapter aqui.
type Hasher struct{ cost int }

var _ ports.PasswordHasher = (*Hasher)(nil)

func New(cost int) *Hasher {
	if cost < bcrypt.MinCost {
		cost = bcrypt.DefaultCost
	}
	return &Hasher{cost: cost}
}

func (h *Hasher) Hash(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), h.cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (h *Hasher) Compare(hashed, plain string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
}

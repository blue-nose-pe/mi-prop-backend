// Package otphash implementa ports.OTPHasher usando HMAC-SHA256 con un
// secret server-side. NO usamos bcrypt porque el espacio (10^6 = 1M)
// es demasiado chico para que el costo de bcrypt sea relevante; el secret
// server-side hace el brute force inviable contra hashes leakeados.
package otphash

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"users_service/internal/core/ports"
)

type Hasher struct {
	secret []byte
}

var _ ports.OTPHasher = (*Hasher)(nil)

// New requiere un secret no vacío. Reutilizar JWT_SECRET es aceptable
// porque ambos viven en el mismo Key Vault y rotaron juntos.
func New(secret string) *Hasher {
	return &Hasher{secret: []byte(secret)}
}

func (h *Hasher) Hash(plain string) string {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write([]byte(plain))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *Hasher) Compare(hashed, plain string) bool {
	return hmac.Equal([]byte(h.Hash(plain)), []byte(hashed))
}

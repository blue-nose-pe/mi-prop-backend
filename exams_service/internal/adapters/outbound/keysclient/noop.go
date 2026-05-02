// Package keysclient implementa ports.KeysClient.
//
// Hoy contiene una implementación NOOP que aprueba todo: útil para dev
// local mientras keys_service no existe. Cuando keys_service esté
// desplegado, reemplazar por GrpcClient (siguiente archivo en este paquete).
package keysclient

import (
	"context"
	"strings"

	"exams_service/internal/core/domain"
	"exams_service/internal/core/ports"
)

// Noop aprueba cualquier código. Solo para dev/testing.
type Noop struct{}

var _ ports.KeysClient = (*Noop)(nil)

func NewNoop() *Noop { return &Noop{} }

func (Noop) Validate(_ context.Context, code, examType string) (*ports.KeyValidation, error) {
	return &ports.KeyValidation{
		KeyID:  domain.KeyID("noop-" + strings.ToLower(examType)),
		OK:     true,
		Reason: "noop client (dev mode)",
	}, nil
}

func (Noop) IncrementUsage(_ context.Context, _ domain.KeyID, _ domain.AttemptID, _ domain.UserID) error {
	return nil
}

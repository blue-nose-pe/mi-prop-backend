// Package query — read side de keys_service.
package query

import (
	"context"

	"keys_service/internal/core/domain"
	"keys_service/internal/core/ports"
	"keys_service/internal/shared/search"
)

type KeyHandler struct {
	keys ports.KeyRepository
}

var _ ports.KeyQueries = (*KeyHandler)(nil)

func NewKeyHandler(keys ports.KeyRepository) *KeyHandler { return &KeyHandler{keys: keys} }

func (h *KeyHandler) Get(ctx context.Context, id domain.KeyID) (*domain.Key, error) {
	return h.keys.FindByID(ctx, id)
}

func (h *KeyHandler) GetByCode(ctx context.Context, code string) (*domain.Key, error) {
	return h.keys.FindByCode(ctx, code)
}

func (h *KeyHandler) ListByAsesor(ctx context.Context, asesorID domain.UserID) ([]domain.Key, error) {
	return h.keys.ListByAsesor(ctx, asesorID)
}

func (h *KeyHandler) ListByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.Key, error) {
	return h.keys.ListByColegio(ctx, schoolID)
}

func (h *KeyHandler) Search(ctx context.Context, req search.Request) (*search.Response, error) {
	return h.keys.Search(ctx, req)
}

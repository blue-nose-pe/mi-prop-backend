// Package query — read side de keys_service.
package query

import (
	"context"

	"keys_service/internal/core/domain"
	"keys_service/internal/core/ports"
	"keys_service/internal/shared/search"
)

type KeyHandler struct {
	keys      ports.KeyRepository
	keyUsages ports.KeyUsageRepository
}

var _ ports.KeyQueries = (*KeyHandler)(nil)

func NewKeyHandler(keys ports.KeyRepository, keyUsages ports.KeyUsageRepository) *KeyHandler {
	return &KeyHandler{keys: keys, keyUsages: keyUsages}
}

func (h *KeyHandler) Get(ctx context.Context, id domain.KeyID) (*domain.Key, error) {
	return h.keys.FindByID(ctx, id)
}

func (h *KeyHandler) GetByCode(ctx context.Context, code string) (*domain.Key, error) {
	return h.keys.FindByCode(ctx, code)
}

func (h *KeyHandler) GetActiveLan(ctx context.Context) (*domain.Key, error) {
	return h.keys.FindActiveLan(ctx)
}

func (h *KeyHandler) ListByAsesor(ctx context.Context, asesorID domain.UserID) ([]domain.Key, error) {
	return h.keys.ListByAsesor(ctx, asesorID)
}

func (h *KeyHandler) ListByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.Key, error) {
	return h.keys.ListByColegio(ctx, schoolID)
}

func (h *KeyHandler) ListAll(ctx context.Context) ([]domain.Key, error) {
	return h.keys.ListAll(ctx)
}

func (h *KeyHandler) ListUserIDsByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.UserID, error) {
	return h.keys.ListUserIDsByColegio(ctx, schoolID)
}

func (h *KeyHandler) ListStudentKeyInfoByColegio(ctx context.Context, schoolID domain.SchoolID) ([]domain.StudentKeyInfo, error) {
	return h.keys.ListStudentKeyInfoByColegio(ctx, schoolID)
}

func (h *KeyHandler) Search(ctx context.Context, req search.Request) (*search.Response, error) {
	return h.keys.Search(ctx, req)
}

func (h *KeyHandler) UserHasKeyUsage(ctx context.Context, keyID domain.KeyID, userID domain.UserID) (bool, error) {
	return h.keyUsages.ExistsByKeyUser(ctx, keyID, userID)
}

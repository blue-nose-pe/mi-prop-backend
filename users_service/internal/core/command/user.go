// Package command contiene los casos de uso que MUTAN estado (CQRS write side).
// Cada handler depende solo de puertos outbound (repos, cache, hasher) y de
// tipos del dominio. NUNCA importa adapters concretos.
package command

import (
	"context"
	"errors"
	"strings"
	"time"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

// UserHandler implementa ports.UserCommands.
// Único responsable de las mutaciones sobre la entidad User.
type UserHandler struct {
	users  ports.UserRepository
	cache  ports.UserCache
	hasher ports.PasswordHasher
}

// Compile-time check: UserHandler cumple ports.UserCommands.
var _ ports.UserCommands = (*UserHandler)(nil)

func NewUserHandler(
	users ports.UserRepository,
	cache ports.UserCache,
	hasher ports.PasswordHasher,
) *UserHandler {
	return &UserHandler{users: users, cache: cache, hasher: hasher}
}

// Create valida unicidad, hashea password e inserta el user.
func (h *UserHandler) Create(ctx context.Context, in ports.CreateUserInput) (*domain.User, error) {
	email := domain.Email(in.Email).Normalize()
	if err := email.Validate(); err != nil {
		return nil, err
	}
	if err := domain.ValidatePasswordStrength(in.Password); err != nil {
		return nil, err
	}

	// Reglas de unicidad — fallar temprano con error de dominio claro.
	// El UNIQUE de la BD sigue siendo el último respaldo.
	if existing, err := h.users.FindByEmail(ctx, email); err == nil && existing != nil {
		return nil, domain.ErrEmailTaken
	} else if err != nil && !errors.Is(err, domain.ErrUserNotFound) {
		return nil, err
	}

	if doc := strings.TrimSpace(in.DocumentNumber); doc != "" {
		exists, err := h.users.ExistsByDocument(ctx, doc)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, domain.ErrDocumentTaken
		}
	}

	hash, err := h.hasher.Hash(in.Password)
	if err != nil {
		return nil, err
	}

	u := &domain.User{
		Email:          email,
		PasswordHash:   hash,
		FirstName:      strings.TrimSpace(in.FirstName),
		LastName:       strings.TrimSpace(in.LastName),
		DocumentNumber: strings.TrimSpace(in.DocumentNumber),
		SchoolID:       domain.SchoolID(strings.TrimSpace(in.SchoolID)),
		Active:         true,
		CreatedAt:      time.Now(),
	}

	id, err := h.users.Save(ctx, u)
	if err != nil {
		return nil, err
	}
	u.ID = id

	_ = h.cache.Set(ctx, u) // best-effort
	return u, nil
}

// Update aplica cambios parciales (campos vacíos no se tocan).
func (h *UserHandler) Update(ctx context.Context, in ports.UpdateUserInput) (*domain.User, error) {
	u, err := h.users.FindByID(ctx, in.ID)
	if err != nil {
		return nil, err
	}

	if v := strings.TrimSpace(in.FirstName); v != "" {
		u.FirstName = v
	}
	if v := strings.TrimSpace(in.LastName); v != "" {
		u.LastName = v
	}
	if v := strings.TrimSpace(in.DocumentNumber); v != "" {
		u.DocumentNumber = v
	}
	if v := strings.TrimSpace(in.SchoolID); v != "" {
		u.SchoolID = domain.SchoolID(v)
	}

	if err := h.users.Update(ctx, u); err != nil {
		return nil, err
	}

	_ = h.cache.Delete(ctx, u.ID) // invalidar para no servir datos viejos
	return u, nil
}

// Deactivate desactiva un user sin borrarlo (preserva histórico).
func (h *UserHandler) Deactivate(ctx context.Context, id domain.UserID) error {
	if err := h.users.SetActive(ctx, id, false); err != nil {
		return err
	}
	_ = h.cache.Delete(ctx, id)
	return nil
}

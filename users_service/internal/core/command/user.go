// Package command contiene los casos de uso que MUTAN estado (CQRS write side).
// Cada handler depende solo de puertos outbound (repos, cache, hasher) y de
// tipos del dominio. NUNCA importa adapters concretos.
package command

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"strings"
	"time"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

// UserHandler implementa ports.UserCommands.
// Único responsable de las mutaciones sobre la entidad User.
type UserHandler struct {
	users   ports.UserRepository
	cache   ports.UserCache
	hasher  ports.PasswordHasher
	hubspot ports.HubspotSyncer // opcional; si es nil, no se sincroniza
}

// Compile-time check: UserHandler cumple ports.UserCommands.
var _ ports.UserCommands = (*UserHandler)(nil)

func NewUserHandler(
	users ports.UserRepository,
	cache ports.UserCache,
	hasher ports.PasswordHasher,
	hubspot ports.HubspotSyncer,
) *UserHandler {
	return &UserHandler{users: users, cache: cache, hasher: hasher, hubspot: hubspot}
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
	h.syncToHubspotAsync(u)
	return u, nil
}

// syncToHubspotAsync hace upsert del contact en HubSpot en una goroutine
// detachada. No bloquea la respuesta al cliente: si HubSpot está caído o
// rate-limited, el user queda creado igual y el sync se intenta después
// vía endpoint manual /api/hubspot/contacts o el job de retry.
func (h *UserHandler) syncToHubspotAsync(u *domain.User) {
	if h.hubspot == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := h.hubspot.UpsertContact(ctx, ports.HubspotContact{
			UserID:    u.ID,
			Email:     string(u.Email),
			DNI:       u.DocumentNumber,
			FirstName: u.FirstName,
			LastName:  u.LastName,
		})
		if err != nil {
			log.Printf("hubspot upsert failed for user=%s err=%v", u.ID, err)
		}
	}()
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
	h.syncToHubspotAsync(u)
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

// Reactivate reactiva un user previamente desactivado.
// Idempotente: si el user ya está activo, no falla.
func (h *UserHandler) Reactivate(ctx context.Context, id domain.UserID) (*domain.User, error) {
	if err := h.users.SetActive(ctx, id, true); err != nil {
		return nil, err
	}
	_ = h.cache.Delete(ctx, id)
	return h.users.FindByID(ctx, id)
}

// ChangePassword: el dueño cambia su propia password.
// Reglas:
//   - Verifica que OldPassword coincida con el hash actual (anti hijack).
//   - Aplica los mismos requisitos de fortaleza que CreateUser.
//   - Limpia must_change_password (lo hace el repo en el mismo UPDATE).
//   - Invalida la cache para no servir el user con el hash viejo.
func (h *UserHandler) ChangePassword(ctx context.Context, in ports.ChangePasswordInput) error {
	u, err := h.users.FindByID(ctx, in.UserID)
	if err != nil {
		return err
	}
	if err := h.hasher.Compare(u.PasswordHash, in.OldPassword); err != nil {
		return domain.ErrInvalidPassword
	}
	if err := domain.ValidatePasswordStrength(in.NewPassword); err != nil {
		return err
	}
	newHash, err := h.hasher.Hash(in.NewPassword)
	if err != nil {
		return err
	}
	if err := h.users.SetPassword(ctx, in.UserID, newHash); err != nil {
		return err
	}
	_ = h.cache.Delete(ctx, in.UserID)
	return nil
}

// ResetPassword: SOLO superadmin. Genera password aleatoria de 16 chars
// server-side (no permite que el admin la elija — evita patrones débiles
// repetidos al resetear muchos users), persiste el hash con
// must_change_password=true y devuelve la temporal en el output.
//
// La temp viaja UNA SOLA VEZ en la respuesta — el admin se la entrega
// al user, que la cambiará en su próximo login.
func (h *UserHandler) ResetPassword(ctx context.Context, in ports.ResetPasswordInput) (*ports.ResetPasswordOutput, error) {
	if !in.ActorIsSuperadmin {
		return nil, domain.ErrSuperadminRequired
	}
	temp, err := generateRandomPassword(16)
	if err != nil {
		return nil, err
	}
	hash, err := h.hasher.Hash(temp)
	if err != nil {
		return nil, err
	}
	if err := h.users.ResetPassword(ctx, in.TargetUserID, hash); err != nil {
		return nil, err
	}
	_ = h.cache.Delete(ctx, in.TargetUserID)
	return &ports.ResetPasswordOutput{TempPassword: temp}, nil
}

// generateRandomPassword devuelve una password de longitud `n` formada
// por caracteres URL-safe base64 (alfanumérico + `-` y `_`). Lee de
// crypto/rand → criptográficamente segura.
//
// 16 chars de base64 ≈ 96 bits de entropía. Más que suficiente para
// password temporal de un solo uso.
func generateRandomPassword(n int) (string, error) {
	if n < 8 {
		n = 8
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b)[:n], nil
}

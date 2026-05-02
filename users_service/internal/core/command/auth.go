package command

import (
	"context"
	"time"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
)

// AuthHandler implementa ports.AuthCommands. Coordina:
//   - Authenticate: verifica credenciales y permisos (sin tokens).
//   - Login:       Authenticate + emite par JWT + persiste refresh.
//   - Refresh:     valida refresh, lo rota, emite nuevo access.
//   - Logout:      revoca refresh.
type AuthHandler struct {
	users        ports.UserRepository
	perms        ports.PermissionRepository
	cache        ports.UserCache
	hasher       ports.PasswordHasher
	tokens       ports.TokenIssuer
	verifier     ports.TokenVerifier
	refreshStore ports.RefreshTokenRepository
}

var _ ports.AuthCommands = (*AuthHandler)(nil)

func NewAuthHandler(
	users ports.UserRepository,
	perms ports.PermissionRepository,
	cache ports.UserCache,
	hasher ports.PasswordHasher,
	tokens ports.TokenIssuer,
	verifier ports.TokenVerifier,
	refreshStore ports.RefreshTokenRepository,
) *AuthHandler {
	return &AuthHandler{
		users:        users,
		perms:        perms,
		cache:        cache,
		hasher:       hasher,
		tokens:       tokens,
		verifier:     verifier,
		refreshStore: refreshStore,
	}
}

// authenticate es el camino común de Authenticate y Login.
// Verifica creds + carga permisos + actualiza last_access.
func (h *AuthHandler) authenticate(ctx context.Context, in ports.AuthenticateInput) (*domain.User, []string, error) {
	email := in.Email.Normalize()

	u, err := h.users.FindByEmail(ctx, email)
	if err != nil {
		// No filtrar "el email no existe": un atacante no debe poder
		// distinguir entre "email inexistente" y "password incorrecto".
		return nil, nil, domain.ErrInvalidPassword
	}
	if !u.Active {
		return nil, nil, domain.ErrUserInactive
	}
	if err := h.hasher.Compare(u.PasswordHash, in.PlainPassword); err != nil {
		return nil, nil, domain.ErrInvalidPassword
	}

	codes, err := h.perms.FindCodesByUserID(ctx, u.ID)
	if err != nil {
		return nil, nil, err
	}

	_ = h.users.TouchLastAccess(ctx, u.ID)
	_ = h.cache.Delete(ctx, u.ID)

	return u, codes, nil
}

// Authenticate (legacy / integraciones internas): credenciales sin tokens.
func (h *AuthHandler) Authenticate(ctx context.Context, in ports.AuthenticateInput) (*ports.AuthenticateOutput, error) {
	u, codes, err := h.authenticate(ctx, in)
	if err != nil {
		return nil, err
	}
	return &ports.AuthenticateOutput{User: u, Permissions: codes}, nil
}

// Login: credenciales → access + refresh JWT. Persiste refresh server-side.
func (h *AuthHandler) Login(ctx context.Context, in ports.LoginInput) (*ports.LoginOutput, error) {
	u, codes, err := h.authenticate(ctx, ports.AuthenticateInput{
		Email:         in.Email,
		PlainPassword: in.PlainPassword,
	})
	if err != nil {
		return nil, err
	}

	pair, err := h.tokens.IssuePair(ports.TokenIssueParams{
		UserID:      u.ID,
		Email:       string(u.Email),
		Permissions: codes,
		SchoolID:    string(u.SchoolID),
		// Roles se derivarán cuando se modelen explícitamente; por ahora
		// los permisos granulares cubren la autorización.
	})
	if err != nil {
		return nil, err
	}

	if err := h.refreshStore.Save(ctx, &ports.RefreshTokenRecord{
		JTI:       pair.RefreshJTI,
		UserID:    u.ID,
		IssuedAt:  time.Now().UTC(),
		ExpiresAt: pair.RefreshExp,
		IP:        in.IP,
		UserAgent: in.UserAgent,
	}); err != nil {
		return nil, err
	}

	return &ports.LoginOutput{
		User:         u,
		Permissions:  codes,
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	}, nil
}

// Refresh rota el refresh token: valida firma + estado en BD, revoca el
// viejo y emite uno nuevo (con nuevo access). Patrón "rotation":
// si un atacante reusa el refresh viejo después de la rotación, el
// servidor lo detecta (revoked_at != NULL) y rechaza.
func (h *AuthHandler) Refresh(ctx context.Context, in ports.RefreshInput) (*ports.RefreshOutput, error) {
	claims, err := h.verifier.Verify(in.RefreshToken)
	if err != nil || claims.Type != "refresh" {
		return nil, domain.ErrInvalidRefreshToken
	}

	rec, err := h.refreshStore.FindByJTI(ctx, claims.JTI)
	if err != nil {
		return nil, err
	}
	if rec.RevokedAt != nil || time.Now().UTC().After(rec.ExpiresAt) {
		return nil, domain.ErrInvalidRefreshToken
	}

	u, err := h.users.FindByID(ctx, rec.UserID)
	if err != nil {
		return nil, err
	}
	if !u.Active {
		// si el user fue desactivado, revocar TODA su familia de refreshes.
		_ = h.refreshStore.RevokeAllForUser(ctx, u.ID)
		return nil, domain.ErrUserInactive
	}

	codes, err := h.perms.FindCodesByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	pair, err := h.tokens.IssuePair(ports.TokenIssueParams{
		UserID:      u.ID,
		Email:       string(u.Email),
		Permissions: codes,
		SchoolID:    string(u.SchoolID),
	})
	if err != nil {
		return nil, err
	}

	// Persiste el nuevo refresh y revoca el viejo apuntándolo al nuevo.
	if err := h.refreshStore.Save(ctx, &ports.RefreshTokenRecord{
		JTI:       pair.RefreshJTI,
		UserID:    u.ID,
		IssuedAt:  time.Now().UTC(),
		ExpiresAt: pair.RefreshExp,
		IP:        in.IP,
		UserAgent: in.UserAgent,
	}); err != nil {
		return nil, err
	}
	if err := h.refreshStore.Revoke(ctx, claims.JTI, pair.RefreshJTI); err != nil {
		return nil, err
	}

	return &ports.RefreshOutput{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	}, nil
}

// Logout revoca el refresh token. Idempotente: si ya estaba revocado
// o no existe, no falla (un logout repetido es operación benigna).
func (h *AuthHandler) Logout(ctx context.Context, in ports.LogoutInput) error {
	claims, err := h.verifier.Verify(in.RefreshToken)
	if err != nil || claims.Type != "refresh" {
		// Token mal formado: tratar como logout exitoso silencioso para
		// no filtrar información a un atacante.
		return nil
	}
	return h.refreshStore.Revoke(ctx, claims.JTI, "")
}

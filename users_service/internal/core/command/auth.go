package command

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"time"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"

	"github.com/google/uuid"
)

// AuthHandler implementa ports.AuthCommands. Coordina:
//   - Authenticate: verifica credenciales y permisos (sin tokens).
//   - Login:       Authenticate + emite par JWT + persiste refresh.
//   - Refresh:     valida refresh, lo rota, emite nuevo access.
//   - Logout:      revoca refresh.
//   - RequestStudentOTP / VerifyStudentOTP: flow de login alternativo
//     para estudiantes (OTP por email vía HubSpot webhook).
type AuthHandler struct {
	users        ports.UserRepository
	perms        ports.PermissionRepository
	cache        ports.UserCache
	hasher       ports.PasswordHasher
	tokens       ports.TokenIssuer
	verifier     ports.TokenVerifier
	refreshStore ports.RefreshTokenRepository
	otpRepo      ports.OTPRepository
	otpHasher    ports.OTPHasher
	otpSender    ports.OTPSender
	classifier   ports.StudentClassifier
	otpTTL       time.Duration
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
	otpRepo ports.OTPRepository,
	otpHasher ports.OTPHasher,
	otpSender ports.OTPSender,
	classifier ports.StudentClassifier,
) *AuthHandler {
	return &AuthHandler{
		users:        users,
		perms:        perms,
		cache:        cache,
		hasher:       hasher,
		tokens:       tokens,
		verifier:     verifier,
		refreshStore: refreshStore,
		otpRepo:      otpRepo,
		otpHasher:    otpHasher,
		otpSender:    otpSender,
		classifier:   classifier,
		otpTTL:       10 * time.Minute,
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

	roles := []string{}
	if u.IsSuperadmin {
		roles = append(roles, "superadmin")
	}
	pair, err := h.tokens.IssuePair(ports.TokenIssueParams{
		UserID:      u.ID,
		Email:       string(u.Email),
		Roles:       roles,
		Permissions: codes,
		SchoolID:    string(u.SchoolID),
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

	roles := []string{}
	if u.IsSuperadmin {
		roles = append(roles, "superadmin")
	}
	pair, err := h.tokens.IssuePair(ports.TokenIssueParams{
		Roles:       roles,
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

// RequestStudentOTP genera un OTP de 6 dígitos, lo persiste hasheado y
// dispara el envío vía hubspot_service. Si el email no existe o el user
// no es estudiante, devuelve OK sin enviar nada (anti enumeration attack).
func (h *AuthHandler) RequestStudentOTP(ctx context.Context, in ports.RequestStudentOTPInput) error {
	email := in.Email.Normalize()

	u, err := h.users.FindByEmail(ctx, email)
	if err != nil {
		// User no existe: OK silencioso.
		return nil
	}
	if !u.Active {
		return nil
	}
	isStudent, err := h.classifier.IsStudent(ctx, u.ID)
	if err != nil {
		return err
	}
	if !isStudent {
		// User existe pero no es estudiante: OK silencioso.
		return nil
	}

	if err := h.otpRepo.InvalidateAllForUser(ctx, u.ID); err != nil {
		return err
	}

	plain, err := generateOTPCode()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	tok := &domain.OTPToken{
		ID:          uuid.NewString(),
		UserID:      u.ID,
		CodeHash:    h.otpHasher.Hash(plain),
		IssuedAt:    now,
		ExpiresAt:   now.Add(h.otpTTL),
		Attempts:    0,
		MaxAttempts: 3,
		IP:          in.IP,
	}
	if err := h.otpRepo.Save(ctx, tok); err != nil {
		return err
	}

	if err := h.otpSender.Send(ctx, string(u.Email), plain); err != nil {
		// Si el envío falla NO retornamos error al cliente: persistimos un
		// log + invalidamos el OTP para que el estudiante pida otro.
		// (alternativa: marcar el row como failed y retentar.)
		log.Printf("RequestStudentOTP: send failed for user=%s err=%v", u.ID, err)
		_ = h.otpRepo.Consume(ctx, tok.ID)
		return domain.ErrOTPDeliveryFail
	}
	return nil
}

// VerifyStudentOTP valida el código y, si match, emite un par access+refresh
// JWT como un Login normal.
func (h *AuthHandler) VerifyStudentOTP(ctx context.Context, in ports.VerifyStudentOTPInput) (*ports.LoginOutput, error) {
	email := in.Email.Normalize()

	u, err := h.users.FindByEmail(ctx, email)
	if err != nil {
		return nil, domain.ErrOTPInvalid
	}
	if !u.Active {
		return nil, domain.ErrUserInactive
	}

	tok, err := h.otpRepo.FindActiveByUser(ctx, u.ID)
	if err != nil {
		return nil, err // ya devuelve ErrOTPInvalid si no hay activo
	}
	if !h.otpHasher.Compare(tok.CodeHash, in.OTP) {
		_ = h.otpRepo.IncrementAttempts(ctx, tok.ID)
		return nil, domain.ErrOTPInvalid
	}

	if err := h.otpRepo.Consume(ctx, tok.ID); err != nil {
		return nil, err
	}

	codes, err := h.perms.FindCodesByUserID(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	_ = h.users.TouchLastAccess(ctx, u.ID)
	_ = h.cache.Delete(ctx, u.ID)

	roles := []string{}
	pair, err := h.tokens.IssuePair(ports.TokenIssueParams{
		UserID:      u.ID,
		Email:       string(u.Email),
		Roles:       roles,
		Permissions: codes,
		SchoolID:    string(u.SchoolID),
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

// generateOTPCode produce 6 dígitos numéricos cripto-aleatorios.
func generateOTPCode() (string, error) {
	const digits = 6
	max := big.NewInt(1_000_000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", digits, n.Int64()), nil
}

package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/core/testutil"
)

func newAuthHandlerWithMocks() (
	*AuthHandler,
	*testutil.UserRepoMock,
	*testutil.PermissionRepoMock,
	*testutil.RefreshRepoMock,
	*testutil.TokenIssuerMock,
	*testutil.TokenVerifierMock,
) {
	users := testutil.NewUserRepoMock()
	perms := testutil.NewPermissionRepoMock()
	cache := testutil.NewUserCacheMock()
	hasher := &testutil.HasherMock{}
	refresh := testutil.NewRefreshRepoMock()
	issuer := &testutil.TokenIssuerMock{}
	verifier := &testutil.TokenVerifierMock{}
	// Tests existentes no ejercitan OTP. Pasamos nil-implementations seguras
	// para los nuevos puertos: stubs que devuelven valores neutrales.
	h := NewAuthHandler(users, stubSchoolRepo{}, perms, cache, hasher, issuer, verifier, refresh,
		stubOTPRepo{}, stubOTPHasher{}, stubOTPSender{}, stubClassifier{}, "")
	return h, users, perms, refresh, issuer, verifier
}

type stubOTPRepo struct{}

func (stubOTPRepo) Save(_ context.Context, _ *domain.OTPToken) error      { return nil }
func (stubOTPRepo) FindActiveByUser(_ context.Context, _ domain.UserID) (*domain.OTPToken, error) {
	return nil, domain.ErrOTPInvalid
}
func (stubOTPRepo) Consume(_ context.Context, _ string) error              { return nil }
func (stubOTPRepo) IncrementAttempts(_ context.Context, _ string) error    { return nil }
func (stubOTPRepo) InvalidateAllForUser(_ context.Context, _ domain.UserID) error { return nil }

type stubOTPHasher struct{}

func (stubOTPHasher) Hash(_ string) string             { return "" }
func (stubOTPHasher) Compare(_ string, _ string) bool  { return false }

type stubOTPSender struct{}

func (stubOTPSender) Send(_ context.Context, _ string, _ string) error { return nil }
func (s stubOTPSender) SendWithContact(ctx context.Context, in ports.OTPSendInput) error {
	return s.Send(ctx, in.Email, in.PlainOTP)
}

// stubSchoolRepo cumple con ports.SchoolRepository sin tocar BD. Solo se
// usa para que NewAuthHandler tenga un puerto valido en tests; ninguno
// de los flujos auth viejos llama a FindByID/List/etc.
type stubSchoolRepo struct{}

func (stubSchoolRepo) FindByID(_ context.Context, _ domain.SchoolID) (*domain.School, error) {
	return nil, domain.ErrSchoolNotFound
}
func (stubSchoolRepo) List(_ context.Context, _ ports.ListSchoolsInput) ([]domain.School, uint32, error) {
	return nil, 0, nil
}
func (stubSchoolRepo) Create(_ context.Context, _ *domain.School) (domain.SchoolID, error) {
	return "", nil
}
func (stubSchoolRepo) Update(_ context.Context, _ *domain.School) error              { return nil }
func (stubSchoolRepo) ListByAsesor(_ context.Context, _ domain.UserID) ([]domain.School, error) {
	return nil, nil
}
func (stubSchoolRepo) SetHubspotRecordID(_ context.Context, _ domain.SchoolID, _ string) error {
	return nil
}

type stubClassifier struct{}

func (stubClassifier) IsStudent(_ context.Context, _ domain.UserID) (bool, error) { return false, nil }

// silence unused-import on ports
var _ ports.OTPRepository = stubOTPRepo{}

func TestAuth_Login_Ok(t *testing.T) {
	h, users, perms, refresh, issuer, _ := newAuthHandlerWithMocks()
	users.Seed(&domain.User{
		ID:           "u1",
		Email:        domain.Email("a@example.com"),
		PasswordHash: "hashed:secret123",
		Active:       true,
	})
	perms.CodesByUser["u1"] = []string{"users.view", "colegios.view"}

	out, err := h.Login(context.Background(), ports.LoginInput{
		Email:         "A@Example.COM",
		PlainPassword: "secret123",
		IP:            "1.2.3.4",
		UserAgent:     "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		t.Fatal("expected non-empty tokens")
	}
	if len(out.Permissions) != 2 {
		t.Errorf("expected 2 permissions, got %d", len(out.Permissions))
	}
	if issuer.IssuedFor == nil || issuer.IssuedFor.UserID != "u1" {
		t.Error("token issuer was not called with the right user")
	}
	// El mock genera jti = "jti-"+UserID; debe estar persistido.
	if rec, err := refresh.FindByJTI(context.Background(), "jti-u1"); err != nil || rec == nil {
		t.Errorf("refresh token not persisted: %v", err)
	}
}

func TestAuth_Login_InvalidPassword(t *testing.T) {
	h, users, _, _, _, _ := newAuthHandlerWithMocks()
	users.Seed(&domain.User{
		ID:           "u1",
		Email:        domain.Email("a@example.com"),
		PasswordHash: "hashed:secret123",
		Active:       true,
	})

	_, err := h.Login(context.Background(), ports.LoginInput{
		Email:         "a@example.com",
		PlainPassword: "wrong",
	})
	if !errors.Is(err, domain.ErrInvalidPassword) {
		t.Errorf("expected ErrInvalidPassword, got %v", err)
	}
}

func TestAuth_Login_UserInactive(t *testing.T) {
	h, users, _, _, _, _ := newAuthHandlerWithMocks()
	users.Seed(&domain.User{
		ID:           "u1",
		Email:        domain.Email("a@example.com"),
		PasswordHash: "hashed:secret123",
		Active:       false,
	})

	_, err := h.Login(context.Background(), ports.LoginInput{
		Email:         "a@example.com",
		PlainPassword: "secret123",
	})
	if !errors.Is(err, domain.ErrUserInactive) {
		t.Errorf("expected ErrUserInactive, got %v", err)
	}
}

func TestAuth_Login_EmailNotFound_LooksLikeBadPassword(t *testing.T) {
	h, _, _, _, _, _ := newAuthHandlerWithMocks()

	_, err := h.Login(context.Background(), ports.LoginInput{
		Email:         "missing@example.com",
		PlainPassword: "whatever",
	})
	// Por seguridad, no debemos filtrar "el email no existe".
	if !errors.Is(err, domain.ErrInvalidPassword) {
		t.Errorf("expected ErrInvalidPassword (no info leak), got %v", err)
	}
}

func TestAuth_Refresh_RotatesToken(t *testing.T) {
	h, users, perms, refresh, issuer, verifier := newAuthHandlerWithMocks()
	users.Seed(&domain.User{ID: "u1", Email: "a@example.com", Active: true})
	perms.CodesByUser["u1"] = []string{"users.view"}

	// El token entrante apunta al jti "jti-old".
	verifier.Claims = &ports.TokenClaims{
		UserID: "u1", JTI: "jti-old", Type: "refresh",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	// Pre-existe ese jti en el repo (vigente).
	_ = refresh.Save(context.Background(), &ports.RefreshTokenRecord{
		JTI:       "jti-old",
		UserID:    "u1",
		IssuedAt:  time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour),
	})

	out, err := h.Refresh(context.Background(), ports.RefreshInput{
		RefreshToken: "irrelevant-mock-verifies",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		t.Fatal("expected new tokens")
	}
	// El viejo debe quedar revocado y apuntando al nuevo jti.
	old, _ := refresh.FindByJTI(context.Background(), "jti-old")
	if old.RevokedAt == nil {
		t.Error("old refresh should be revoked after rotation")
	}
	if old.ReplacedBy == "" {
		t.Error("old refresh should reference the new jti")
	}
	if issuer.IssuedFor == nil || issuer.IssuedFor.UserID != "u1" {
		t.Error("new pair should be issued for u1")
	}
}

func TestAuth_Refresh_RejectsRevokedToken(t *testing.T) {
	h, users, _, refresh, _, verifier := newAuthHandlerWithMocks()
	users.Seed(&domain.User{ID: "u1", Email: "a@example.com", Active: true})
	verifier.Claims = &ports.TokenClaims{
		UserID: "u1", JTI: "jti-revoked", Type: "refresh",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	now := time.Now()
	_ = refresh.Save(context.Background(), &ports.RefreshTokenRecord{
		JTI:       "jti-revoked",
		UserID:    "u1",
		IssuedAt:  now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(time.Hour),
		RevokedAt: &now,
	})

	_, err := h.Refresh(context.Background(), ports.RefreshInput{RefreshToken: "x"})
	if !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Errorf("expected ErrInvalidRefreshToken, got %v", err)
	}
}

func TestAuth_Refresh_RejectsWrongTokenType(t *testing.T) {
	h, _, _, _, _, verifier := newAuthHandlerWithMocks()
	// Un access token llega por error.
	verifier.Claims = &ports.TokenClaims{
		UserID: "u1", JTI: "jti", Type: "access",
		ExpiresAt: time.Now().Add(time.Hour),
	}

	_, err := h.Refresh(context.Background(), ports.RefreshInput{RefreshToken: "x"})
	if !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Errorf("expected ErrInvalidRefreshToken, got %v", err)
	}
}

func TestAuth_Logout_Idempotent(t *testing.T) {
	h, _, _, _, _, verifier := newAuthHandlerWithMocks()
	verifier.Claims = &ports.TokenClaims{
		UserID: "u1", JTI: "jti-logout", Type: "refresh",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	// No persistido, pero Logout no debe fallar (idempotencia).
	if err := h.Logout(context.Background(), ports.LogoutInput{RefreshToken: "x"}); err != nil {
		t.Errorf("Logout should be idempotent, got %v", err)
	}
}

func TestAuth_Logout_TolerantToInvalidToken(t *testing.T) {
	h, _, _, _, _, verifier := newAuthHandlerWithMocks()
	verifier.Err = errors.New("bad token")

	if err := h.Logout(context.Background(), ports.LogoutInput{RefreshToken: "garbage"}); err != nil {
		t.Errorf("Logout with invalid token should silently succeed, got %v", err)
	}
}

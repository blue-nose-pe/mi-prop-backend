package command

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/url"
	"strings"
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
	schools      ports.SchoolRepository // para resolver school.IntID en sendStudentOTPFull
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
	// frontBaseURL: base del front (ej https://...) para armar el magic link
	// del correo masivo (<front>/test/simulacro/ingresar?e=&c=). "" = no se
	// incluye el boton (el alumno usa el codigo manual). Viene de FRONT_BASE_URL.
	frontBaseURL string
}

var _ ports.AuthCommands = (*AuthHandler)(nil)

func NewAuthHandler(
	users ports.UserRepository,
	schools ports.SchoolRepository,
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
	frontBaseURL string,
) *AuthHandler {
	return &AuthHandler{
		users:        users,
		schools:      schools,
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
		frontBaseURL: strings.TrimRight(frontBaseURL, "/"),
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
		UserID:             u.ID,
		Email:              string(u.Email),
		Roles:              roles,
		Permissions:        codes,
		SchoolID:           string(u.SchoolID),
		MustChangePassword: u.MustChangePassword,
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
		Roles:              roles,
		UserID:             u.ID,
		Email:              string(u.Email),
		Permissions:        codes,
		SchoolID:           string(u.SchoolID),
		MustChangePassword: u.MustChangePassword,
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
	// SEGURIDAD (audit 2026-06-18): se quito el log [DEBUG OTP] que imprimia el
	// OTP en TEXTO PLANO en los logs del pod → account-takeover de menores dentro
	// del TTL de 10 min para cualquiera con acceso a `kubectl logs`. El correo del
	// OTP se entrega por SMTP (OTP_SENDER=smtp en prod), el log ya no hace falta.
	// Observabilidad sin el secreto (solo el user id opaco):
	log.Printf("[otp] generated user=%s (delivery via configured sender)", u.ID)

	if err := h.otpSender.Send(ctx, string(u.Email), plain); err != nil {
		// Bug 4 fix (anti-enumeration): NO devolver error al cliente.
		// Antes retornaba ErrOTPDeliveryFail (500) para emails de estudiantes
		// validos cuando el envio fallaba, mientras emails inexistentes
		// devolvian 200 OK. Un atacante podia diferenciar (estudiante real
		// con falla) de (no existe). Ahora tragamos el error, log interno,
		// y el cliente recibe 200 OK uniforme.
		log.Printf("RequestStudentOTP: send failed for user=%s err=%v", u.ID, err)
		_ = h.otpRepo.Consume(ctx, tok.ID)
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
		UserID:             u.ID,
		Email:              string(u.Email),
		Roles:              roles,
		Permissions:        codes,
		SchoolID:           string(u.SchoolID),
		MustChangePassword: u.MustChangePassword,
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

// RegisterStudentWithKey crea (o reusa) un usuario estudiante asociado al
// colegio resuelto por el caller (gateway, que valido la key antes), y
// dispara el envio del OTP. Idempotente para emails ya existentes que
// pertenezcan al mismo colegio.
//
// Errores devueltos:
//   - domain.ErrEmailTaken: email existe pero el user NO es estudiante o
//     pertenece a otro colegio.
//   - domain.ErrValidation: payload invalido (email mal formado, sin
//     school_id, sin first/last name).
//   - domain.ErrPermGroupNotFound: el grupo "student_permissions" no
//     existe — bug de seed o BD vacia.
func (h *AuthHandler) RegisterStudentWithKey(
	ctx context.Context,
	in ports.RegisterStudentWithKeyInput,
) (*ports.RegisterStudentWithKeyOutput, error) {
	email := in.Email.Normalize()
	if err := email.Validate(); err != nil {
		return nil, err
	}
	// Bug #4 fix: school_id puede ser "" cuando la key es LAN (modo
	// masivo sin colegio asociado). El campo users.school_id es nullable
	// en BD desde la migracion 002, asi que dejamos que el estudiante
	// quede sin colegio. Quien valido la key ya verifico que es usable.
	firstName := strings.TrimSpace(in.FirstName)
	lastName := strings.TrimSpace(in.LastName)
	if firstName == "" || lastName == "" {
		return nil, fmt.Errorf("first_name and last_name are required")
	}

	// Colegio DESACTIVADO ("en reserva"): no admite NUEVOS registros ni re-alta
	// de alumnos (mismo criterio que crear llaves, gateway keys.go C1). El caso
	// LAN/masivo (in.SchoolID == "") no tiene colegio y no se bloquea.
	if in.SchoolID != "" && h.schools != nil {
		if sch, serr := h.schools.FindByID(ctx, in.SchoolID); serr == nil && sch != nil && !sch.Active {
			return nil, domain.ErrSchoolInactive
		}
	}

	// Resuelve el grupo de estudiantes por code estable. Si no existe es
	// bug de seed (migration 014) — devolvemos error visible para que el
	// operador se entere.
	group, err := h.perms.FindGroupByCode(ctx, "student_permissions")
	if err != nil {
		return nil, fmt.Errorf("student_permissions group: %w", err)
	}

	existing, lookupErr := h.users.FindByEmail(ctx, email)
	if lookupErr == nil && existing != nil {
		// Email ya existe → validar que sea estudiante del mismo colegio.
		isStudent, err := h.classifier.IsStudent(ctx, existing.ID)
		if err != nil {
			return nil, err
		}
		if !isStudent {
			// Bug 5 fix (anti-enumeration): emails registrados como NO-estudiante
			// (asesor/admin/coordinador) antes devolvian ErrEmailTaken (409),
			// emails libres devolvian "created" (200), y estudiantes del mismo
			// colegio "exists" (200). Un atacante podia descubrir si un email
			// era staff probando con su propia key. Ahora devolvemos "exists"
			// silencioso (no disparamos OTP) y loggeamos internamente.
			log.Printf("RegisterStudentWithKey: email %s registered as non-student, ignoring", email)
			return &ports.RegisterStudentWithKeyOutput{Status: "exists", UserID: existing.ID}, nil
		}
		if string(existing.SchoolID) != string(in.SchoolID) {
			// Bug 5 fix: misma idea — estudiante de otro colegio devuelve
			// "exists" sin disparar OTP (no migramos school_id por seguridad
			// — ver comentario hijack). El user real igual puede usar
			// /api/auth/student/request-otp + /verify-otp con su colegio
			// original.
			log.Printf("RegisterStudentWithKey: email %s belongs to other school, ignoring (existing=%s, requested=%s)",
				email, existing.SchoolID, in.SchoolID)
			return &ports.RegisterStudentWithKeyOutput{Status: "exists", UserID: existing.ID}, nil
		}
		// Reactivar si estaba desactivado (estudiante volvio).
		if !existing.Active {
			if err := h.users.SetActive(ctx, existing.ID, true); err != nil {
				return nil, err
			}
		}
		// Mismo flujo que RequestStudentOTP: generar codigo, persistir,
		// disparar envio. Pasamos los datos del payload por si son mas
		// completos que los del user existente (ej: el front rellena
		// document_number/phone en el re-registro y nuestra BD los tenia
		// vacios del registro anterior).
		if err := h.sendStudentOTPFull(ctx, existing, ports.OTPSendInput{
			FirstName:      strings.TrimSpace(firstOrFallback(in.FirstName, existing.FirstName)),
			LastName:       strings.TrimSpace(firstOrFallback(in.LastName, existing.LastName)),
			DocumentNumber: strings.TrimSpace(firstOrFallback(in.DocumentNumber, existing.DocumentNumber)),
			Phone:          strings.TrimSpace(firstOrFallback(in.Phone, existing.Phone)),
			SchoolID:       string(existing.SchoolID),
			WantMagicLink:  string(in.SchoolID) == "", // masivo (key LAN, sin colegio)
		}, in.IP); err != nil {
			return nil, err
		}
		return &ports.RegisterStudentWithKeyOutput{
			Status: "exists",
			UserID: existing.ID,
		}, nil
	}
	if lookupErr != nil && !errors.Is(lookupErr, domain.ErrUserNotFound) {
		return nil, lookupErr
	}

	// User no existe → crearlo. Password random (16 chars) porque el
	// estudiante no la va a usar nunca; el login es solo por OTP.
	randomPwd, err := generateRandomPassword(16)
	if err != nil {
		return nil, err
	}
	pwdHash, err := h.hasher.Hash(randomPwd)
	if err != nil {
		return nil, err
	}
	u := &domain.User{
		Email:          email,
		PasswordHash:   pwdHash,
		FirstName:      firstName,
		LastName:       lastName,
		DocumentNumber: strings.TrimSpace(in.DocumentNumber),
		Phone:          strings.TrimSpace(in.Phone),
		SchoolID:       in.SchoolID,
		Active:         true,
		CreatedAt:      time.Now(),
	}
	id, err := h.users.Save(ctx, u)
	if err != nil {
		return nil, err
	}
	u.ID = id

	// Asignar el grupo student_permissions. Si esto falla, eliminar el
	// user que acabamos de crear seria lo ideal, pero el repo no expone
	// Delete (y deactivate dejaria basura). En la practica AssignGroup
	// solo falla si el grupo no existe o por error de BD; ambos son
	// problemas operativos que ameritan investigar, no rollback.
	if err := h.perms.AssignGroupToUser(ctx, id, group.ID); err != nil {
		return nil, err
	}

	if err := h.sendStudentOTPFull(ctx, u, ports.OTPSendInput{
		FirstName:      firstName,
		LastName:       lastName,
		DocumentNumber: strings.TrimSpace(in.DocumentNumber),
		Phone:          strings.TrimSpace(in.Phone),
		SchoolID:       string(in.SchoolID),
		WantMagicLink:  string(in.SchoolID) == "", // masivo (key LAN, sin colegio)
	}, in.IP); err != nil {
		return nil, err
	}

	return &ports.RegisterStudentWithKeyOutput{
		Status: "created",
		UserID: id,
	}, nil
}

// sendStudentOTP centraliza la generacion + persist + envio del OTP.
// Variante "minimal" — solo email + otp, sin datos del estudiante. La
// usa RequestStudentOTP (login posterior) donde el contacto ya esta en
// HubSpot con info completa del registro inicial.
func (h *AuthHandler) sendStudentOTP(ctx context.Context, u *domain.User, ip string) error {
	return h.sendStudentOTPFull(ctx, u, ports.OTPSendInput{}, ip)
}

// sendStudentOTPFull es la variante con datos del estudiante. La usa
// RegisterStudentWithKey para que hubspot_service upsertee el contacto
// con nombre/apellido/dni/phone, evitando que aparezca vacio en CRM.
// Si extra.FirstName/... vienen vacios, el adapter ignora los campos
// y la upsert solo refresca otp_estudiante (mismo efecto que el flow
// minimal de Request OTP).
func (h *AuthHandler) sendStudentOTPFull(ctx context.Context, u *domain.User, extra ports.OTPSendInput, ip string) error {
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
		IP:          ip,
	}
	if err := h.otpRepo.Save(ctx, tok); err != nil {
		return err
	}
	// SEGURIDAD (audit 2026-06-18): removido el log que imprimia el OTP en texto
	// plano (riesgo de account-takeover de menores dentro del TTL). El correo se
	// entrega por SMTP. El OTP plano sigue disponible SOLO en `extra.PlainOTP`
	// (abajo) para armar el magic link del flujo masivo — eso NO se loguea.
	log.Printf("[otp] generated user=%s (delivery via configured sender)", u.ID)
	extra.Email = string(u.Email)
	extra.PlainOTP = plain
	// MASIVO (landing "Preparate"): armamos el magic link con el OTP embebido
	// para que el alumno entre directo desde el correo (fiel a prod). Solo si
	// el caller lo pidio (registro masivo, key LAN) y hay front configurado.
	if extra.WantMagicLink && h.frontBaseURL != "" {
		extra.MagicLinkURL = h.frontBaseURL + "/test/simulacro/ingresar?e=" +
			url.QueryEscape(string(u.Email)) + "&c=" + url.QueryEscape(plain)
	}
	// Resolver datos del colegio para que hubspot_service pueda popular
	// la prop mi_proposito___id_colegio (INTEGER, P1 usaba autoincr de
	// MySQL) y asociar Contact <-> Company. Best-effort: si falla la
	// query, ambos campos quedan vacios y hubspot_service los ignora.
	if u.SchoolID != "" && h.schools != nil {
		if sch, err := h.schools.FindByID(ctx, u.SchoolID); err == nil && sch != nil {
			extra.SchoolIntID = sch.IntID
			extra.SchoolRecordID = sch.HubspotRecordID
		}
	}
	if err := h.otpSender.SendWithContact(ctx, extra); err != nil {
		log.Printf("sendStudentOTP: send failed for user=%s err=%v", u.ID, err)
		_ = h.otpRepo.Consume(ctx, tok.ID)
		return domain.ErrOTPDeliveryFail
	}
	return nil
}

// firstOrFallback devuelve a si no es vacio (tras trim), sino b. Usado
// para preferir lo que vino del payload del request sobre lo que ya
// estaba en BD cuando ambos coexisten (re-registro).
func firstOrFallback(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
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

// CheckStudentEmail responde si el email corresponde a un estudiante
// activo. Devuelve (false, "", nil) para emails desconocidos, inactivos,
// o que pertenezcan a usuarios no-estudiantes. El gateway solo invoca
// este metodo despues de validar un key_code, asi que la superficie
// anti-enumeration sigue cerrada: para preguntar necesitas el secret de
// la key.
//
// Cuando existe=true, retorna tambien el user_id — el gateway lo necesita
// para chequear cuota de attempts contra una key sin exponer ListByUser
// publicamente.
func (h *AuthHandler) CheckStudentEmail(ctx context.Context, email domain.Email) (bool, domain.UserID, error) {
	u, err := h.users.FindByEmail(ctx, email.Normalize())
	if err != nil {
		return false, "", nil
	}
	if !u.Active {
		return false, "", nil
	}
	isStudent, err := h.classifier.IsStudent(ctx, u.ID)
	if err != nil {
		return false, "", err
	}
	if !isStudent {
		return false, "", nil
	}
	return true, u.ID, nil
}

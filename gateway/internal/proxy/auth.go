// Auth handlers — proxy a users-service AuthService.
//
// Rutas REST:
//   POST /api/auth/login                       → AuthService.Login
//   POST /api/auth/refresh                     → AuthService.Refresh
//   POST /api/auth/logout                      → AuthService.Logout
//   POST /api/auth/student/request-otp         → AuthService.RequestStudentOTP
//   POST /api/auth/student/verify-otp          → AuthService.VerifyStudentOTP
//   POST /api/auth/student/register-with-key   → keys.ValidateKey + AuthService.RegisterStudentWithKey
//   POST /api/auth/student/lookup-by-key       → keys.ValidateKey + AuthService.CheckStudentEmail
package proxy

import (
	"net/http"

	examsgrpcpb "exams_service/proto/gen"
	keysgrpcpb "keys_service/proto/gen"
	usersgrpcpb "users_service/proto/gen"
)

func (p *Proxy) RegisterAuth(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", p.login)
	mux.HandleFunc("POST /api/auth/refresh", p.refresh)
	mux.HandleFunc("POST /api/auth/logout", p.logout)
	mux.HandleFunc("POST /api/auth/student/request-otp", p.requestStudentOTP)
	mux.HandleFunc("POST /api/auth/student/verify-otp", p.verifyStudentOTP)
	mux.HandleFunc("POST /api/auth/student/register-with-key", p.registerStudentWithKey)
	mux.HandleFunc("POST /api/auth/student/lookup-by-key", p.lookupStudentByKey)
}

// lookupStudentByKey — chequeo previo del flujo /test/{simulacro|vocacional|eda}/acceso.
//
// Body: { email, key_code }
// 200:  {
//         exists: bool,                  // email registrado como estudiante activo
//         can_attempt: bool,             // false si ya consumio key.max_attempts_per_user
//         max_attempts_per_user: int32,  // cuota de la key (0 = sin limite)
//         submitted_count: int32,        // cuantos attempts SUBMITTED tiene con esta key
//         last_attempt_id: string        // ultimo attempt previo (si lo hay) para redirigir a /resultados
//       }
// 4xx:  KEY_NOT_FOUND / KEY_NOT_USABLE si la key no es valida o esta vencida.
//
// Ruta publica (jwtSkip lo deja pasar sin Authorization). Anti-enumeration:
// el caller necesita poseer un key_code valido — no se puede iterar emails
// sin tener una key activa primero.
//
// Lo usa el front para tres decisiones:
//   - exists=false           → mostrar "Registrate con tu llave".
//   - exists=true, can_attempt=true  → mandar OTP normal.
//   - exists=true, can_attempt=false → BLOQUEAR sin mandar OTP, mostrar
//     mensaje "Ya rendiste el simulacro" en la pantalla de acceso.
type lookupStudentByKeyRequest struct {
	Email   string `json:"email"`
	KeyCode string `json:"key_code"`
}

func (p *Proxy) lookupStudentByKey(w http.ResponseWriter, r *http.Request) {
	var in lookupStudentByKeyRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if in.Email == "" || in.KeyCode == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status:  "error",
			Code:    "VALIDATION_ERROR",
			Message: "email y key_code son obligatorios",
		})
		return
	}

	// 1) Validar key — sin esto NO respondemos sobre el email (anti-enumeration).
	keyResp, err := p.cli.Keys.ValidateKey(r.Context(), &keysgrpcpb.ValidateKeyRequest{Code: in.KeyCode})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	key := keyResp.GetKey()

	// 2) Preguntar a users-service si el email es un estudiante activo.
	resp, err := p.cli.Auth.CheckStudentEmail(r.Context(), &usersgrpcpb.CheckStudentEmailRequest{Email: in.Email})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	out := map[string]any{
		"exists":                resp.GetExists(),
		"can_attempt":           true,
		"max_attempts_per_user": int32(0),
		"submitted_count":       int32(0),
		"last_attempt_id":       "",
	}

	// 3) Quota check: solo si el alumno existe + la key tiene limite por usuario.
	// Si no hay limite (max_attempts_per_user=0) o el alumno no existe (ira a
	// registro), no consultamos exams_service.
	if resp.GetExists() && key != nil && key.GetMaxAttemptsPerUser() > 0 {
		out["max_attempts_per_user"] = key.GetMaxAttemptsPerUser()
		countResp, err := p.cli.Attempts.CountSubmittedByKeyUser(r.Context(), &examsgrpcpb.CountSubmittedByKeyUserRequest{
			KeyId:  key.GetId(),
			UserId: resp.GetUserId(),
		})
		if err != nil {
			// Si exams falla, NO bloqueamos el flow — caemos al chequeo del
			// back en StartAttempt. Mejor que mostrar OTP que pre-bloquear
			// erroneamente.
			writeJSON(w, http.StatusOK, out)
			return
		}
		out["submitted_count"] = countResp.GetCount()
		if countResp.GetCount() >= key.GetMaxAttemptsPerUser() {
			out["can_attempt"] = false
			out["last_attempt_id"] = countResp.GetLastAttemptId()
		}
	}

	writeJSON(w, http.StatusOK, out)
}

type loginRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	UserAgent string `json:"user_agent,omitempty"`
}
type loginResponse struct {
	User         any      `json:"user"`
	Permissions  []string `json:"permissions"`
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
}

func (p *Proxy) login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Auth.Login(r.Context(), &usersgrpcpb.LoginRequest{
		Email:     in.Email,
		Password:  in.Password,
		Ip:        clientIPFromRequest(r),
		UserAgent: firstNonEmpty(in.UserAgent, r.Header.Get("User-Agent")),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	perms := resp.GetPermissions()
	if perms == nil {
		perms = []string{}
	}
	writeJSON(w, http.StatusOK, loginResponse{
		User:         protoUserToJSON(resp.GetUser()),
		Permissions:  perms,
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}
type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (p *Proxy) refresh(w http.ResponseWriter, r *http.Request) {
	var in refreshRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Auth.Refresh(r.Context(), &usersgrpcpb.RefreshRequest{
		RefreshToken: in.RefreshToken,
		Ip:           clientIPFromRequest(r),
		UserAgent:    r.Header.Get("User-Agent"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, refreshResponse{
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
	})
}

func (p *Proxy) logout(w http.ResponseWriter, r *http.Request) {
	var in refreshRequest // mismo body
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.Auth.Logout(r.Context(), &usersgrpcpb.LogoutRequest{
		RefreshToken: in.RefreshToken,
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ----- Student OTP flow (login alternativo para estudiantes) -----
//
//   POST /api/auth/student/request-otp { email }            → 200 {status:"ok"}
//   POST /api/auth/student/verify-otp  { email, otp }       → 200 {user, permissions, access_token, refresh_token}
//
// Comportamiento de seguridad: request-otp siempre devuelve 200 OK aunque
// el email no exista o el user no sea estudiante (anti enumeration attack).
// El email solo llega si el user es realmente estudiante.

type requestStudentOTPRequest struct {
	Email string `json:"email"`
}

type verifyStudentOTPRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

func (p *Proxy) requestStudentOTP(w http.ResponseWriter, r *http.Request) {
	var in requestStudentOTPRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if _, err := p.cli.Auth.RequestStudentOTP(r.Context(), &usersgrpcpb.RequestStudentOTPRequest{
		Email: in.Email,
		Ip:    clientIPFromRequest(r),
	}); err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (p *Proxy) verifyStudentOTP(w http.ResponseWriter, r *http.Request) {
	var in verifyStudentOTPRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	resp, err := p.cli.Auth.VerifyStudentOTP(r.Context(), &usersgrpcpb.VerifyStudentOTPRequest{
		Email:     in.Email,
		Otp:       in.OTP,
		Ip:        clientIPFromRequest(r),
		UserAgent: r.Header.Get("User-Agent"),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	perms := resp.GetPermissions()
	if perms == nil {
		perms = []string{}
	}
	writeJSON(w, http.StatusOK, loginResponse{
		User:         protoUserToJSON(resp.GetUser()),
		Permissions:  perms,
		AccessToken:  resp.GetAccessToken(),
		RefreshToken: resp.GetRefreshToken(),
	})
}

// ----- Auto-registro de estudiante con key publica -----
//
//   POST /api/auth/student/register-with-key
//   body: { email, first_name, last_name, document_number, phone, key_code }
//   200 { status: "created" | "exists" } — OTP enviado en ambos casos
//   400/401/422: errores de validacion de key, email mal formado, etc.
//
// Flow:
//   1. ValidateKey(key_code) en keys_service: chequea active + vigencia +
//      aforo restante. Read-only — NO incrementa current_uses.
//   2. Si OK, llamamos a users_service.AuthService.RegisterStudentWithKey
//      pasando email + datos personales + school_id resuelto de la key.
//   3. users_service crea (o reusa) el user con permission_group=
//      student_permissions y dispara el OTP.
//
// Ruta publica (jwtSkip lo deja pasar sin Authorization).

type registerStudentWithKeyRequest struct {
	Email          string `json:"email"`
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	DocumentNumber string `json:"document_number"`
	Phone          string `json:"phone"`
	KeyCode        string `json:"key_code"`
}

func (p *Proxy) registerStudentWithKey(w http.ResponseWriter, r *http.Request) {
	var in registerStudentWithKeyRequest
	if err := readJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if in.Email == "" || in.FirstName == "" || in.LastName == "" || in.KeyCode == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{
			Status:  "error",
			Code:    "VALIDATION_ERROR",
			Message: "email, first_name, last_name y key_code son obligatorios",
		})
		return
	}

	// 1) Validar key. Si la key no es valida, devolvemos el error tal cual
	// (keys_service lo distingue: KEY_NOT_FOUND, KEY_EXPIRED, KEY_EXHAUSTED).
	keyResp, err := p.cli.Keys.ValidateKey(r.Context(), &keysgrpcpb.ValidateKeyRequest{Code: in.KeyCode})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	key := keyResp.GetKey()
	if key == nil {
		writeJSON(w, http.StatusNotFound, errorBody{
			Status:  "error",
			Code:    "KEY_NOT_FOUND",
			Message: "La llave indicada no existe o no esta disponible.",
		})
		return
	}
	// Bug #4 fix: P1 soportaba auto-registro masivo con keys LAN (sin
	// colegio asignado) — flujo "LAN##" del Manual del administrador. Si
	// `key.school_id == ""`, igual permitimos el registro; el estudiante
	// quedara con `users.school_id = NULL`. Los dashboards de colegio
	// no lo veran (es lo esperado para flujo masivo), pero el alumno
	// puede rendir el examen.
	schoolID := key.GetSchoolId()

	// 2) Delegar creacion + envio de OTP a users_service.
	regResp, err := p.cli.Auth.RegisterStudentWithKey(r.Context(), &usersgrpcpb.RegisterStudentWithKeyRequest{
		Email:          in.Email,
		FirstName:      in.FirstName,
		LastName:       in.LastName,
		DocumentNumber: in.DocumentNumber,
		Phone:          in.Phone,
		SchoolId:       schoolID,
		Ip:             clientIPFromRequest(r),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": regResp.GetStatus(),
	})
}

// ----- helpers locales -----

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func clientIPFromRequest(r *http.Request) string {
	// Bug 12 fix: NGINX Ingress sobrescribe X-Real-Ip con el IP del cliente
	// real (no es prependible por el cliente porque NGINX la reemplaza).
	// X-Forwarded-For en cambio es una cadena que el cliente puede prefijar
	// libremente — si un atacante manda X-Forwarded-For: 8.8.8.8 burla el
	// rate-limit por IP y ofusca los logs de auditoria.
	//
	// Priorizamos X-Real-Ip (NGINX), caemos a r.RemoteAddr (que en cluster
	// es la IP del pod NGINX), y X-Forwarded-For solo como ultimo recurso
	// para entornos sin Ingress (dev local). Esto cierra el spoof trivial
	// sin requerir cambiar la config de NGINX/APIM.
	if v := r.Header.Get("X-Real-Ip"); v != "" {
		return v
	}
	if v := r.RemoteAddr; v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		for i := 0; i < len(v); i++ {
			if v[i] == ',' {
				return v[:i]
			}
		}
		return v
	}
	return ""
}

// protoUserToJSON convierte un *userspb.User a un map JSON-friendly.
// Centralizamos para no repetir en cada handler que devuelva User.
func protoUserToJSON(u *usersgrpcpb.User) map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{
		"id":                   u.GetId(),
		"email":                u.GetEmail(),
		"first_name":           u.GetFirstName(),
		"last_name":            u.GetLastName(),
		"document_number":      u.GetDocumentNumber(),
		"phone":                u.GetPhone(),
		"school_id":            u.GetSchoolId(),
		"active":               u.GetActive(),
		"last_access_at":       optionalTimestamp(u.GetLastAccessAt()),
		"created_at":            optionalTimestamp(u.GetCreatedAt()),
		"updated_at":           optionalTimestamp(u.GetUpdatedAt()),
		"is_superadmin":        u.GetIsSuperadmin(),
		"must_change_password": u.GetMustChangePassword(),
	}
}

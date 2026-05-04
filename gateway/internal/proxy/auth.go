// Auth handlers — proxy a users-service AuthService.
//
// Rutas REST:
//   POST /api/auth/login                  → AuthService.Login
//   POST /api/auth/refresh                → AuthService.Refresh
//   POST /api/auth/logout                 → AuthService.Logout
//   POST /api/auth/student/request-otp    → AuthService.RequestStudentOTP
//   POST /api/auth/student/verify-otp     → AuthService.VerifyStudentOTP
package proxy

import (
	"net/http"

	usersgrpcpb "users_service/proto/gen"
)

func (p *Proxy) RegisterAuth(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", p.login)
	mux.HandleFunc("POST /api/auth/refresh", p.refresh)
	mux.HandleFunc("POST /api/auth/logout", p.logout)
	mux.HandleFunc("POST /api/auth/student/request-otp", p.requestStudentOTP)
	mux.HandleFunc("POST /api/auth/student/verify-otp", p.verifyStudentOTP)
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
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		for i := 0; i < len(v); i++ {
			if v[i] == ',' {
				return v[:i]
			}
		}
		return v
	}
	if v := r.Header.Get("X-Real-Ip"); v != "" {
		return v
	}
	return r.RemoteAddr
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
		"school_id":            u.GetSchoolId(),
		"active":               u.GetActive(),
		"last_access_at":       optionalTimestamp(u.GetLastAccessAt()),
		"created_at":            optionalTimestamp(u.GetCreatedAt()),
		"updated_at":           optionalTimestamp(u.GetUpdatedAt()),
		"is_superadmin":        u.GetIsSuperadmin(),
		"must_change_password": u.GetMustChangePassword(),
	}
}

// Package middleware — MustChangePasswordGuard.
package middleware

import (
	"net/http"
	"strings"
)

// MustChangePasswordGuard rechaza con 403 PASSWORD_CHANGE_REQUIRED cualquier
// request cuyo JWT lleve claim mcp=true, excepto las rutas en `allowPrefixes`
// (tipicamente /api/users/me/change-password, /api/users/me, /api/auth/logout
// y /health).
//
// Fix de BUG #31: el bootstrap del admin via helm Job crea el user con
// must_change_password=1; el endpoint POST /api/users/me/change-password
// limpia el flag (UPDATE users SET password_hash=..., must_change_password=0).
// Sin este guard el admin podia usar la pass random del helm secret para
// siempre, sin rotar.
//
// Se aplica DESPUES del middleware JWT (necesita claims). Las rutas en
// jwtSkip (publicas) ya no llegan aca.
func MustChangePasswordGuard(allowPrefixes []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil || !claims.MustChangePassword {
				next.ServeHTTP(w, r)
				return
			}
			// Allow-list: el user PUEDE acceder a estos endpoints para
			// poder rotar la pass y luego seguir.
			for _, p := range allowPrefixes {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeJSON(w, http.StatusForbidden,
				`{"status":"error","code":"PASSWORD_CHANGE_REQUIRED","message":"debes cambiar tu contrasena antes de usar la plataforma"}`)
		})
	}
}

package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims son los mismos que emite users-service.
type JWTClaims struct {
	Email       string   `json:"email,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	SchoolID    string   `json:"school_id,omitempty"`
	Type        string   `json:"type"`
	jwt.RegisteredClaims
}

const claimsCtxKey ctxKey = 100

// ClaimsFromContext recupera los claims insertados por el middleware JWT.
// Devuelve nil si la request no requirió auth (skip list).
func ClaimsFromContext(ctx context.Context) *JWTClaims {
	v, _ := ctx.Value(claimsCtxKey).(*JWTClaims)
	return v
}

// JWT valida el header Authorization: Bearer <token>. Si la ruta está
// en `skip`, lo saltea (ej. /api/auth/login no requiere JWT).
//
// Importante: gateway NO emite tokens, solo valida. Quien los emite es
// users-service AuthService — el gateway delega allí cuando recibe
// /api/auth/login.
func JWT(secret []byte, issuer string, skipPrefixes []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r.URL.Path, skipPrefixes) {
				next.ServeHTTP(w, r)
				return
			}
			authz := r.Header.Get("Authorization")
			if !strings.HasPrefix(authz, "Bearer ") {
				writeJSON(w, http.StatusUnauthorized,
					`{"status":"error","code":"MISSING_BEARER","message":"missing or invalid Authorization header"}`)
				return
			}
			token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))

			claims := &JWTClaims{}
			tok, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, http.ErrAbortHandler
				}
				return secret, nil
			}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(issuer))
			if err != nil || !tok.Valid {
				writeJSON(w, http.StatusUnauthorized,
					`{"status":"error","code":"INVALID_TOKEN","message":"invalid or expired token"}`)
				return
			}
			if claims.Type != "access" {
				writeJSON(w, http.StatusUnauthorized,
					`{"status":"error","code":"WRONG_TOKEN_TYPE","message":"access token required"}`)
				return
			}
			ctx := context.WithValue(r.Context(), claimsCtxKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func shouldSkip(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

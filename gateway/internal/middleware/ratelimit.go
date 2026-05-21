package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimit por IP usando Redis (sliding window con INCR + EXPIRE).
// Token bucket sería más correcto bajo bursts, pero para ciudad-tier
// (UCSP, ~5k requests/día) sliding window es suficiente y trivial.
//
// Si rdb es nil → middleware noop (deshabilitado).
func RateLimit(rdb *redis.Client, perMinute int) Middleware {
	if rdb == nil || perMinute <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			key := fmt.Sprintf("rl:ip:%s:%d", ip, time.Now().Unix()/60)
			ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
			defer cancel()

			count, err := rdb.Incr(ctx, key).Result()
			if err != nil {
				// Redis caído → fail-open: prefiero servir tráfico que tirar
				// el sistema. El log alerta al operador.
				next.ServeHTTP(w, r)
				return
			}
			if count == 1 {
				_ = rdb.Expire(ctx, key, 90*time.Second).Err()
			}
			if count > int64(perMinute) {
				w.Header().Set("Retry-After", "60")
				writeJSON(w, http.StatusTooManyRequests,
					`{"status":"error","code":"RATE_LIMIT","message":"too many requests"}`)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP devuelve el IP del cliente real. Bug 12 fix: priorizamos
// X-Real-Ip que NGINX Ingress reescribe (no prependible por el cliente).
// X-Forwarded-For era trivialmente spoofable para evadir rate-limit por IP.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Real-Ip"); v != "" {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if v := r.RemoteAddr; v != "" {
		return v
	}
	// Fallback solo para entornos sin Ingress (dev local).
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

// Package middleware — middlewares HTTP del gateway. Cada uno es una
// función `func(http.Handler) http.Handler` componible vía Chain.
package middleware

import (
	"context"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Middleware es la firma estándar net/http.
type Middleware func(http.Handler) http.Handler

// Chain compone middlewares de izquierda a derecha. El primero es el más
// externo (Recovery debe ir primero para capturar panics de los demás).
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// Recovery convierte panics en HTTP 500 sin tumbar el server.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("PANIC %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				http.Error(w, `{"status":"error","message":"internal server error"}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CorrelationID inyecta x-correlation-id si no viene del cliente y lo
// expone como header de respuesta para tracing end-to-end.
type ctxKey int

const correlationKey ctxKey = iota

func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cid := r.Header.Get("X-Correlation-Id")
		if cid == "" {
			cid = uuid.NewString()
		}
		w.Header().Set("X-Correlation-Id", cid)
		ctx := context.WithValue(r.Context(), correlationKey, cid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CorrelationFromContext recupera el cid (utilizado por proxy → gRPC).
func CorrelationFromContext(ctx context.Context) string {
	v, _ := ctx.Value(correlationKey).(string)
	return v
}

// Logging: una línea por request. status code lo capturamos con un
// ResponseWriter wrapper. Pino-friendly cuando se cambia logger.
type loggingWriter struct {
	http.ResponseWriter
	status int
}

func (lw *loggingWriter) WriteHeader(code int) {
	lw.status = code
	lw.ResponseWriter.WriteHeader(code)
}

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lw := &loggingWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		log.Printf("[%s] %s %s %d %v",
			CorrelationFromContext(r.Context()), r.Method, r.URL.Path, lw.status, time.Since(start))
	})
}

// CORS valida origin contra una whitelist. Para preflight (OPTIONS)
// responde 204 directo. Lo demás pasa con headers Access-Control-*.
func CORS(allowed []string) Middleware {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		allowedSet[strings.TrimSpace(o)] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				if _, ok := allowedSet[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
			}
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Correlation-Id")
				w.Header().Set("Access-Control-Expose-Headers", "X-Correlation-Id")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

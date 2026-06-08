// gateway — punto de entrada HTTP del sistema. Traduce REST → gRPC.
//
// Detrás de Azure APIM (que se encarga de policies de tráfico globales)
// y un Ingress NGINX (que termina TLS). gateway corre dentro del cluster
// AKS con auth JWT obligatoria salvo en la skip-list.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gateway/config"
	"gateway/internal/middleware"
	"gateway/internal/proxy"

	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	if cfg.JWTSecret == "" {
		log.Fatalf("JWT_SECRET is required")
	}

	// ---------- 1. gRPC clients ----------
	clients, err := proxy.Dial(proxy.Addrs{
		Users:        cfg.UsersServiceAddr,
		Exams:        cfg.ExamsServiceAddr,
		Keys:         cfg.KeysServiceAddr,
		Hubspot:      cfg.HubspotServiceAddr,
		Satisfaction: cfg.SatisfactionServiceAddr,
		Analytics:    cfg.AnalyticsServiceAddr,
	})
	if err != nil {
		log.Fatalf("dial upstreams: %v", err)
	}
	defer clients.Close()

	// ---------- 2. Redis para rate-limit (opcional) ----------
	var rdb *redis.Client
	if cfg.RateLimitEnabled {
		rdb = redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
		})
		defer rdb.Close()
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			log.Printf("[warn] redis unavailable; rate-limit disabled: %v", err)
			rdb = nil
		}
	}

	// ---------- 3. Mux + middlewares ----------
	mux := http.NewServeMux()
	pr := proxy.NewWithRedis(clients, rdb)
	pr.RegisterAll(mux)

	// JWT skip-list: rutas que no requieren auth.
	jwtSkip := []string{
		"/api/auth/login",
		"/api/auth/refresh",
		"/api/auth/student/request-otp",
		"/api/auth/student/verify-otp",
		"/api/auth/student/register-with-key",
		"/api/auth/student/lookup-by-key",
		// El estudiante valida su key ANTES de pedir OTP (vista
		// /test/simulacro/acceso). Sin esto, GET by-code devolvia 401
		// MISSING_BEARER y el front mostraba "No pudimos validar tu llave"
		// indistintamente para no-tengo-cuenta y para ingreso. El secreto es
		// el propio code: si lo conoces, ya puedes usarlo — exponer metadata
		// no abre superficie real.
		"/api/keys/by-code/",
		"/api/careers",
		// Cliente (doc observaciones): el alumno anonimo (acceso por key+OTP,
		// sin JWT) ve la encuesta de satisfaccion al terminar el examen y la
		// envia. Rutas publicas dedicadas que solo tocan encuestas PUBLICADAS.
		"/api/public/",
		"/health",
	}

	// Allow-list del MustChangePasswordGuard: rutas accesibles aun cuando
	// el JWT lleva mcp=true. El user puede leer su perfil (/me), cambiar
	// su pass, y cerrar sesion. Cualquier otra ruta devuelve 403.
	mcpAllow := []string{
		"/api/users/me/change-password",
		"/api/users/me",
		"/api/auth/logout",
		"/health",
	}

	handler := middleware.Chain(mux,
		middleware.Recovery,
		middleware.CorrelationID,
		middleware.Logging,
		middleware.CORS(cfg.CORSAllowedOrigins),
		middleware.RateLimit(rdb, cfg.RateLimitPerIPPerMinute),
		middleware.JWT([]byte(cfg.JWTSecret), cfg.JWTIssuer, jwtSkip),
		middleware.MustChangePasswordGuard(mcpAllow),
	)

	srv := &http.Server{
		Addr:              cfg.HTTPPort,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ---------- 4. Run + graceful shutdown ----------
	errCh := make(chan error, 1)
	go func() {
		log.Printf("gateway listening on %s", cfg.HTTPPort)
		errCh <- srv.ListenAndServe()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("http server: %v", err)
	case <-sig:
		log.Printf("shutdown signal received, draining...")
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownWait)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

// Composition Root de analytics_service.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"analytics_service/config"
	grpchandler "analytics_service/internal/adapters/inbound/grpc"
	cacheadapter "analytics_service/internal/adapters/outbound/cache"
	"analytics_service/internal/adapters/outbound/clients"
	excelexport "analytics_service/internal/adapters/outbound/excel"
	"analytics_service/internal/core/ports"
	"analytics_service/internal/core/query"
	"analytics_service/internal/shared/jwtmw"
	pb "analytics_service/proto/gen"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg := config.Load()
	if cfg.JWTSecret == "" {
		log.Fatalf("JWT_SECRET is required")
	}

	// ---------- 1. Cache (opcional) ----------
	var cache ports.Cache
	if cfg.CacheEnabled {
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		defer rdb.Close()
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			log.Printf("[warn] redis ping failed (cache disabled): %v", err)
		} else {
			cache = cacheadapter.NewRedis(rdb)
		}
	}

	// ---------- 2. Upstream clients ----------
	// Si la env var *_SERVICE_ADDR está seteada, usamos el cliente gRPC
	// real; si está vacía, fallback al Noop (útil para dev local sin
	// los servicios upstream corriendo). El error en NewGrpc* solo
	// fallará si addr es "", que ya filtramos arriba — un dial fallido
	// más tarde devuelve error en el primer RPC, no acá.
	var usersC ports.UsersClient = clients.NoopUsers{}
	if cfg.UsersServiceAddr != "" {
		gu, err := clients.NewGrpcUsers(cfg.UsersServiceAddr)
		if err != nil {
			log.Fatalf("init users client: %v", err)
		}
		defer func() { _ = gu.Close() }()
		usersC = gu
		log.Printf("[wire] users client: gRPC -> %s", cfg.UsersServiceAddr)
	} else {
		log.Printf("[wire] users client: NoOp (USERS_SERVICE_ADDR vacío)")
	}

	var examsC ports.ExamsClient = clients.NoopExams{}
	if cfg.ExamsServiceAddr != "" {
		ge, err := clients.NewGrpcExams(cfg.ExamsServiceAddr)
		if err != nil {
			log.Fatalf("init exams client: %v", err)
		}
		defer func() { _ = ge.Close() }()
		examsC = ge
		log.Printf("[wire] exams client: gRPC -> %s", cfg.ExamsServiceAddr)
	} else {
		log.Printf("[wire] exams client: NoOp (EXAMS_SERVICE_ADDR vacío)")
	}

	var keysC ports.KeysClient = clients.NoopKeys{}
	if cfg.KeysServiceAddr != "" {
		gk, err := clients.NewGrpcKeys(cfg.KeysServiceAddr)
		if err != nil {
			log.Fatalf("init keys client: %v", err)
		}
		defer func() { _ = gk.Close() }()
		keysC = gk
		log.Printf("[wire] keys client: gRPC -> %s", cfg.KeysServiceAddr)
	} else {
		log.Printf("[wire] keys client: NoOp (KEYS_SERVICE_ADDR vacío)")
	}

	// ---------- 3. Core ----------
	dashboards := query.NewDashboardHandler(usersC, examsC, keysC, cache)
	exporter := excelexport.NewExporter(dashboards)

	// ---------- 4. gRPC server ----------
	lis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	verifier := jwtmw.NewVerifier([]byte(cfg.JWTSecret), cfg.JWTIssuer)
	jwtSkip := func(fullMethod string) bool {
		return strings.HasPrefix(fullMethod, "/grpc.health.") ||
			strings.HasPrefix(fullMethod, "/grpc.reflection.")
	}

	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpchandler.RecoveryInterceptor,
			grpchandler.CorrelationIDInterceptor,
			grpchandler.LoggingInterceptor,
			jwtmw.UnaryServerInterceptor(verifier, jwtSkip),
		),
	)
	pb.RegisterAnalyticsServiceServer(s, grpchandler.NewAnalyticsHandler(dashboards, exporter))

	hs := health.NewServer()
	healthpb.RegisterHealthServer(s, hs)
	hs.SetServingStatus("analytics.v1.AnalyticsService", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(s)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("analytics_service gRPC listening on %s", cfg.GRPCPort)
		errCh <- s.Serve(lis)
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("grpc server: %v", err)
	case <-sig:
		log.Printf("shutdown signal received, draining...")
		done := make(chan struct{})
		go func() { s.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-time.After(cfg.ShutdownWait):
			log.Printf("graceful stop timeout, forcing")
			s.Stop()
		}
	}
}

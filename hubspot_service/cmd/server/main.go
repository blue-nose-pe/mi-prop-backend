// Composition Root del SERVER de hubspot_service:
//   - gRPC server (lo consumen users_service y exams_service).
//   - HTTP webhook server (lo invoca HubSpot ante cambios en su lado).
// El procesamiento de jobs encolados vive en cmd/worker/main.go (proceso
// distinto, escalable independiente).
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"hubspot_service/config"
	grpchandler "hubspot_service/internal/adapters/inbound/grpc"
	httpinbound "hubspot_service/internal/adapters/inbound/http"
	asynqadapter "hubspot_service/internal/adapters/outbound/asynq"
	auditadapter "hubspot_service/internal/adapters/outbound/audit"
	hubspotclient "hubspot_service/internal/adapters/outbound/hubspot"
	"hubspot_service/internal/adapters/outbound/usersclient"
	webhookadapter "hubspot_service/internal/adapters/outbound/webhook"
	"hubspot_service/internal/core/command"
	"hubspot_service/internal/core/query"
	"hubspot_service/internal/shared/auditmw"
	"hubspot_service/internal/shared/jwtmw"
	"hubspot_service/internal/shared/requestsengine"
	pb "hubspot_service/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func main() {
	cfg := config.Load()
	tokens := cfg.EffectiveTokens()
	if len(tokens) == 0 || cfg.OTPWebhookToken == "" {
		log.Fatalf("HUBSPOT_API_TOKEN(S) and HUBSPOT_OTP_WEBHOOK_TOKEN are required")
	}
	if cfg.JWTSecret == "" {
		log.Fatalf("JWT_SECRET is required")
	}

	// ---------- 1. Outbound deps ----------
	// Engine: rate limit distribuido vía Redis. Coordina entre TODAS las
	// réplicas de hubspot-service (server + worker) para no superar
	// el RPS por API key (HubSpot = 10 rps/key) sin importar cuántas
	// réplicas tenga el Deployment.
	engine, err := requestsengine.New(requestsengine.Config{
		Tokens:        tokens,
		RPS:           cfg.HubspotRPS,
		Workers:       cfg.HubspotWorkers,
		QueueSize:     cfg.HubspotQueueSize,
		RedisAddr:     cfg.RedisAddr,
		RedisPassword: cfg.RedisPassword,
		RedisDB:       cfg.RedisDBRateLimit,
		KeyPrefix:     "hs_ratelimit",
	})
	if err != nil {
		log.Fatalf("requestsengine: %v", err)
	}
	defer engine.Close()

	hsClient := hubspotclient.New(engine)
	otpWebhook := webhookadapter.NewOTP(cfg.OTPWebhookTriggerID, cfg.OTPWebhookToken)
	enqueuer := asynqadapter.NewEnqueuer(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisTLS)
	defer enqueuer.Close()
	jobsAdmin := asynqadapter.NewAdmin(cfg.RedisAddr, cfg.RedisPassword)
	usersCB := usersclient.NewNoop() // TODO: gRPC real cuando users_service exponga el RPC

	auditSink := auditadapter.NewStdoutSink()

	// ---------- 2. Core (CQRS) ----------
	syncCmds := command.NewSyncHandler(hsClient, otpWebhook, enqueuer, usersCB)
	syncQrys := query.NewSyncHandler(hsClient, jobsAdmin)
	adminCmds := command.NewAdminHandler(jobsAdmin)

	// ---------- 3. Inbound: gRPC server ----------
	grpcLis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		log.Fatalf("listen grpc: %v", err)
	}

	verifier := jwtmw.NewVerifier([]byte(cfg.JWTSecret), cfg.JWTIssuer)
	jwtSkip := func(fullMethod string) bool {
		return strings.HasPrefix(fullMethod, "/grpc.health.") ||
			strings.HasPrefix(fullMethod, "/grpc.reflection.")
	}

	gs := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpchandler.RecoveryInterceptor,
			grpchandler.CorrelationIDInterceptor,
			grpchandler.LoggingInterceptor,
			jwtmw.UnaryServerInterceptor(verifier, jwtSkip),
			auditmw.UnaryServerInterceptor(auditadapter.NewBridge(auditSink), redactSensitiveFields),
		),
	)
	pb.RegisterHubspotServiceServer(gs, grpchandler.NewHubspotHandler(syncCmds, syncQrys, adminCmds))

	hs := health.NewServer()
	healthpb.RegisterHealthServer(gs, hs)
	hs.SetServingStatus("hubspot.v1.HubspotService", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(gs)

	// ---------- 4. Inbound: HTTP webhook ----------
	httpSrv := &http.Server{
		Addr:              cfg.WebhookPort,
		Handler:           httpinbound.NewMux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ---------- 5. Run + graceful shutdown ----------
	errCh := make(chan error, 2)
	go func() {
		log.Printf("hubspot_service gRPC listening on %s", cfg.GRPCPort)
		errCh <- gs.Serve(grpcLis)
	}()
	go func() {
		log.Printf("hubspot_service HTTP webhook listening on %s", cfg.WebhookPort)
		errCh <- httpSrv.ListenAndServe()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("server error: %v", err)
	case <-sig:
		log.Printf("shutdown signal received, draining...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownWait)
		defer cancel()

		done := make(chan struct{})
		go func() { gs.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-shutdownCtx.Done():
			log.Printf("graceful stop timeout, forcing")
			gs.Stop()
		}
		_ = httpSrv.Shutdown(shutdownCtx)
	}
}

// ----- audit sanitizer -----

var sensitiveFieldNames = map[string]struct{}{
	"otp":           {},
	"token":         {},
	"refresh_token": {},
	"access_token":  {},
	"password":      {},
}

func redactSensitiveFields(_ string, req proto.Message) proto.Message {
	if req == nil {
		return nil
	}
	clone := proto.Clone(req)
	m := clone.ProtoReflect()
	m.Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if _, ok := sensitiveFieldNames[string(fd.Name())]; ok && fd.Kind() == protoreflect.StringKind {
			m.Clear(fd)
		}
		return true
	})
	return clone
}

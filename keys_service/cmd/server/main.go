// Composition Root de keys_service.
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

	"keys_service/config"
	grpchandler "keys_service/internal/adapters/inbound/grpc"
	auditadapter "keys_service/internal/adapters/outbound/audit"
	hubspotclient "keys_service/internal/adapters/outbound/hubspotclient"
	mssqladapter "keys_service/internal/adapters/outbound/mssql"
	"keys_service/internal/core/command"
	"keys_service/internal/core/ports"
	"keys_service/internal/core/query"
	"keys_service/internal/shared/auditmw"
	"keys_service/internal/shared/jwtmw"
	"keys_service/internal/shared/mssql"
	pb "keys_service/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func main() {
	cfg := config.Load()
	if cfg.JWTSecret == "" {
		log.Fatalf("JWT_SECRET is required (mount via Key Vault CSI)")
	}

	ctx := context.Background()

	db, err := mssql.Open(ctx, mssql.Config{
		Server:          cfg.SQLServer,
		Port:            cfg.SQLPort,
		Database:        cfg.SQLDatabase,
		User:            cfg.SQLUser,
		Password:        cfg.SQLPassword,
		Encrypt:         cfg.SQLEncrypt,
		TrustServerCert: cfg.SQLTrustServerCert,
		AppName:         "keys-service",
	})
	if err != nil {
		log.Fatalf("mssql open: %v", err)
	}
	defer db.Close()

	// Outbound adapters
	keys := mssqladapter.NewKeyRepo(db)
	usages := mssqladapter.NewKeyUsageRepo(db)
	auditSink := mssqladapter.NewAuditSink(db)

	// Hubspot syncer — best-effort goroutine desde Generate/Update para
	// replicar el custom object Key al CRM (paridad con P1). Si
	// HUBSPOT_SERVICE_ADDR esta vacio cae a NoopSyncer (dev local).
	var hubspotSyncer ports.HubspotSyncer = hubspotclient.NoopSyncer{}
	if cfg.HubspotServiceAddr != "" {
		hs, err := hubspotclient.NewGrpc(cfg.HubspotServiceAddr)
		if err != nil {
			log.Fatalf("hubspot client dial: %v", err)
		}
		defer hs.Close()
		hubspotSyncer = hs
		log.Printf("[hubspot] gRPC client → %s", cfg.HubspotServiceAddr)
	} else {
		log.Printf("[hubspot] NoopSyncer (HUBSPOT_SERVICE_ADDR vacio)")
	}

	// Core (CQRS)
	keyCmds := command.NewKeyHandler(keys, usages, hubspotSyncer)
	keyQrys := query.NewKeyHandler(keys, usages)

	// Inbound (gRPC)
	keyHandler := grpchandler.NewKeyHandler(keyCmds, keyQrys)

	// Server
	lis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	verifier := jwtmw.NewVerifier([]byte(cfg.JWTSecret), cfg.JWTIssuer)
	jwtSkip := func(fullMethod string) bool {
		// ValidateKey y GetByCode son publicos: ambos los invoca el flujo
		// anonimo del estudiante en /test/simulacro/acceso ANTES de pedir
		// OTP (validar key → mostrar tipo de test / activa / vigente).
		// Read-only; no exponen info mas alla de lo que ya revela conocer
		// el secret code.
		return strings.HasPrefix(fullMethod, "/grpc.health.") ||
			strings.HasPrefix(fullMethod, "/grpc.reflection.") ||
			fullMethod == "/keys.v1.KeyService/ValidateKey" ||
			fullMethod == "/keys.v1.KeyService/GetByCode" ||
			fullMethod == "/keys.v1.KeyService/UserHasKeyUsage"
	}

	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpchandler.RecoveryInterceptor,
			grpchandler.CorrelationIDInterceptor,
			grpchandler.LoggingInterceptor,
			jwtmw.UnaryServerInterceptor(verifier, jwtSkip),
			auditmw.UnaryServerInterceptor(auditadapter.NewBridge(auditSink), redactSensitiveFields),
		),
	)
	pb.RegisterKeyServiceServer(s, keyHandler)

	hs := health.NewServer()
	healthpb.RegisterHealthServer(s, hs)
	hs.SetServingStatus("keys.v1.KeyService", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(s)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("keys_service gRPC listening on %s", cfg.GRPCPort)
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

var sensitiveFieldNames = map[string]struct{}{
	"password":      {},
	"refresh_token": {},
	"access_token":  {},
	"token":         {},
	"otp":           {},
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

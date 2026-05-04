// Composition Root de satisfaction_service.
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

	"satisfaction_service/config"
	grpchandler "satisfaction_service/internal/adapters/inbound/grpc"
	auditadapter "satisfaction_service/internal/adapters/outbound/audit"
	mssqladapter "satisfaction_service/internal/adapters/outbound/mssql"
	"satisfaction_service/internal/core/command"
	"satisfaction_service/internal/core/query"
	"satisfaction_service/internal/shared/auditmw"
	"satisfaction_service/internal/shared/jwtmw"
	"satisfaction_service/internal/shared/mssql"
	pb "satisfaction_service/proto/gen"

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
		AppName:         "satisfaction-service",
	})
	if err != nil {
		log.Fatalf("mssql open: %v", err)
	}
	defer db.Close()

	surveys := mssqladapter.NewSurveyRepo(db)
	questions := mssqladapter.NewQuestionRepo(db)
	responses := mssqladapter.NewResponseRepo(db)
	auditSink := mssqladapter.NewAuditSink(db)

	surveyCmds := command.NewSurveyHandler(surveys, questions)
	responseCmds := command.NewResponseHandler(surveys, questions, responses)
	surveyQrys := query.NewSurveyHandler(surveys, questions)
	responseQrys := query.NewResponseHandler(responses)

	surveyHandler := grpchandler.NewSurveyHandler(surveyCmds, surveyQrys)
	responseHandler := grpchandler.NewResponseHandler(responseCmds, responseQrys)

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
			auditmw.UnaryServerInterceptor(auditadapter.NewBridge(auditSink), redactSensitiveFields),
		),
	)
	pb.RegisterSurveyServiceServer(s, surveyHandler)
	pb.RegisterResponseServiceServer(s, responseHandler)

	hs := health.NewServer()
	healthpb.RegisterHealthServer(s, hs)
	hs.SetServingStatus("satisfaction.v1.SurveyService", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("satisfaction.v1.ResponseService", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(s)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("satisfaction_service gRPC listening on %s", cfg.GRPCPort)
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

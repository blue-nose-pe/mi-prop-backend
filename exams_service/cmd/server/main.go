// Composition Root de exams_service.
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

	"exams_service/config"
	grpchandler "exams_service/internal/adapters/inbound/grpc"
	auditadapter "exams_service/internal/adapters/outbound/audit"
	"exams_service/internal/adapters/outbound/keysclient"
	mssqladapter "exams_service/internal/adapters/outbound/mssql"
	"exams_service/internal/core/command"
	"exams_service/internal/core/ports"
	"exams_service/internal/core/query"
	"exams_service/internal/shared/auditmw"
	"exams_service/internal/shared/jwtmw"
	"exams_service/internal/shared/mssql"
	pb "exams_service/proto/gen"

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

	// ---------- 1. Infrastructure ----------
	db, err := mssql.Open(ctx, mssql.Config{
		Server:          cfg.SQLServer,
		Port:            cfg.SQLPort,
		Database:        cfg.SQLDatabase,
		User:            cfg.SQLUser,
		Password:        cfg.SQLPassword,
		Encrypt:         cfg.SQLEncrypt,
		TrustServerCert: cfg.SQLTrustServerCert,
		AppName:         "exams-service",
	})
	if err != nil {
		log.Fatalf("mssql open: %v", err)
	}
	defer db.Close()

	// ---------- 2. Outbound adapters ----------
	examTypes := mssqladapter.NewExamTypeRepo(db)
	exams := mssqladapter.NewExamRepo(db)
	questions := mssqladapter.NewQuestionRepo(db)
	options := mssqladapter.NewOptionRepo(db)
	examQuestions := mssqladapter.NewExamQuestionRepo(db)
	attempts := mssqladapter.NewAttemptRepo(db)
	auditSink := mssqladapter.NewAuditSink(db)

	// keys_service: si KEYS_SERVICE_ADDR está seteado usamos el GrpcClient real;
	// si no, fallback a Noop para dev local sin keys_service.
	var keys ports.KeysClient
	if cfg.KeysServiceAddr != "" {
		gc, err := keysclient.NewGrpc(cfg.KeysServiceAddr)
		if err != nil {
			log.Fatalf("keysclient grpc dial: %v", err)
		}
		defer gc.Close()
		keys = gc
		log.Printf("[keysclient] gRPC client → %s", cfg.KeysServiceAddr)
	} else {
		keys = keysclient.NewNoop()
		log.Printf("[keysclient] NoOp (KEYS_SERVICE_ADDR vacío)")
	}

	// ---------- 3. Core (CQRS) ----------
	examCmds := command.NewExamHandler(examTypes, exams, examQuestions)
	questionCmds := command.NewQuestionHandler(questions, options)
	examQuestionCmds := command.NewExamQuestionHandler(exams, examQuestions)
	attemptCmds := command.NewAttemptHandler(exams, examQuestions, options, questions, attempts)

	examQrys := query.NewExamHandler(exams)
	questionQrys := query.NewQuestionHandler(questions, options)
	examQuestionQrys := query.NewExamQuestionHandler(examQuestions)
	attemptQrys := query.NewAttemptHandler(attempts)

	// ---------- 4. Inbound adapters (gRPC) ----------
	examHandler := grpchandler.NewExamHandler(examCmds, examQrys)
	questionHandler := grpchandler.NewQuestionHandler(questionCmds, questionQrys)
	examQuestionHandler := grpchandler.NewExamQuestionHandler(examQuestionCmds, examQuestionQrys)
	attemptHandler := grpchandler.NewAttemptHandler(attemptCmds, attemptQrys, keys)

	// ---------- 5. Server ----------
	lis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	verifier := jwtmw.NewVerifier([]byte(cfg.JWTSecret), cfg.JWTIssuer)
	jwtSkip := func(fullMethod string) bool {
		// /exams.v1.AttemptService/CountSubmittedByKeyUser es publico para
		// que el gateway lo invoque en /api/auth/student/lookup-by-key
		// (preflight ANTES del OTP, sin alumno autenticado todavia).
		if fullMethod == "/exams.v1.AttemptService/CountSubmittedByKeyUser" {
			return true
		}
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

	pb.RegisterExamServiceServer(s, examHandler)
	pb.RegisterQuestionServiceServer(s, questionHandler)
	pb.RegisterExamQuestionServiceServer(s, examQuestionHandler)
	pb.RegisterAttemptServiceServer(s, attemptHandler)

	hs := health.NewServer()
	healthpb.RegisterHealthServer(s, hs)
	hs.SetServingStatus("exams.v1.ExamService", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	reflection.Register(s)

	// ---------- 6. Run + graceful shutdown ----------
	errCh := make(chan error, 1)
	go func() {
		log.Printf("exams_service gRPC listening on %s", cfg.GRPCPort)
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

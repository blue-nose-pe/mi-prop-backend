// Composition Root. ÚNICO lugar donde las piezas concretas se conectan.
// Si mañana cambiamos Azure SQL por otra DB, Redis por Memcached o bcrypt
// por argon2, solo se toca este archivo.
// rebuild marker 2026-05-04
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

	"users_service/config"
	grpchandler "users_service/internal/adapters/inbound/grpc"
	auditadapter "users_service/internal/adapters/outbound/audit"
	bcryptadapter "users_service/internal/adapters/outbound/bcrypt"
	jwtadapter "users_service/internal/adapters/outbound/jwt"
	hubspotsyncadapter "users_service/internal/adapters/outbound/hubspotsync"
	mssqladapter "users_service/internal/adapters/outbound/mssql"
	otphashadapter "users_service/internal/adapters/outbound/otphash"
	otpsenderadapter "users_service/internal/adapters/outbound/otpsender"
	redisadapter "users_service/internal/adapters/outbound/redis"
	"users_service/internal/core/command"
	"users_service/internal/core/ports"
	"users_service/internal/core/query"
	"users_service/internal/shared/auditmw"
	"users_service/internal/shared/jwtmw"
	"users_service/internal/shared/mssql"
	"users_service/internal/shared/permmw"
	pb "users_service/proto/gen"

	hubspotpb "hubspot_service/proto/gen"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg := config.Load()
	if cfg.JWTSecret == "" {
		log.Fatalf("JWT_SECRET is required (mount via Key Vault CSI)")
	}

	// ---------- 1. INFRAESTRUCTURA ----------
	ctx := context.Background()

	db, err := mssql.Open(ctx, mssql.Config{
		Server:          cfg.SQLServer,
		Port:            cfg.SQLPort,
		Database:        cfg.SQLDatabase,
		User:            cfg.SQLUser,
		Password:        cfg.SQLPassword,
		Encrypt:         cfg.SQLEncrypt,
		TrustServerCert: cfg.SQLTrustServerCert,
		AppName:         "users-service",
	})
	if err != nil {
		log.Fatalf("mssql open: %v", err)
	}
	defer db.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis ping: %v", err)
	}

	// ---------- 2. ADAPTERS OUTBOUND ----------
	userRepo := mssqladapter.NewUserRepo(db)
	permRepo := mssqladapter.NewPermissionRepo(db)
	schoolRepo := mssqladapter.NewSchoolRepo(db)
	visitaRepo := mssqladapter.NewVisitaRepo(db)
	refreshRepo := mssqladapter.NewRefreshTokenRepo(db)
	assignmentRepo := mssqladapter.NewAssignmentRepo(db)
	otpRepo := mssqladapter.NewOTPRepo(db)
	studentClassifier := mssqladapter.NewStudentClassifier(db)
	auditSink := mssqladapter.NewAuditSink(db)
	cache := redisadapter.NewUserCache(rdb, cfg.CacheTTL)
	hasher := bcryptadapter.New(cfg.BcryptCost)
	otpHasher := otphashadapter.New(cfg.JWTSecret)

	// OTP sender + HubSpot sync.
	//
	// OTP_SENDER=resend  → correo directo via Resend HTTP. El sync de
	//                      contacto a HubSpot se hace best-effort en
	//                      goroutine usando hubspot_service.SyncStudentContact
	//                      (no bloquea login si HubSpot esta caido).
	// OTP_SENDER=hubspot → path antiguo: hubspot_service.SendOTP dispara
	//                      el Workflow del portal que manda el email.
	//
	// Si HUBSPOT_SERVICE_ADDR esta vacio, el sender cae a NoOp salvo que
	// OTP_SENDER=resend (en cuyo caso Resend funciona standalone).
	var otpSender ports.OTPSender = noopOTPSender{}
	var hubspotSync ports.HubspotSyncer
	var hubspotGRPCCli hubspotpb.HubspotServiceClient // para inyectar al Resend adapter
	if cfg.HubspotServiceAddr != "" {
		// Mantenemos los dos clientes del path antiguo porque
		// hubspotSyncadapter (UpsertContact) lo usa el flujo de admin
		// fuera del OTP.
		gs, err := otpsenderadapter.NewGrpc(cfg.HubspotServiceAddr)
		if err != nil {
			log.Fatalf("otpsender grpc dial: %v", err)
		}
		defer gs.Close()
		hubspotGRPCCli = gs.Client()
		if strings.EqualFold(cfg.OTPSender, "hubspot") {
			otpSender = gs
			log.Printf("[otpsender] hubspot gRPC client → %s", cfg.HubspotServiceAddr)
		}

		hs, err := hubspotsyncadapter.NewGrpc(cfg.HubspotServiceAddr)
		if err != nil {
			log.Fatalf("hubspotsync grpc dial: %v", err)
		}
		defer hs.Close()
		hubspotSync = hs
		log.Printf("[hubspotsync] gRPC client → %s", cfg.HubspotServiceAddr)
	} else {
		log.Printf("[hubspotsync] disabled (HUBSPOT_SERVICE_ADDR vacío)")
	}

	if strings.EqualFold(cfg.OTPSender, "resend") {
		if cfg.ResendAPIKey == "" {
			log.Fatalf("OTP_SENDER=resend requiere RESEND_API_KEY")
		}
		otpSender = otpsenderadapter.NewResend(cfg.ResendAPIKey, cfg.ResendFromEmail, cfg.ResendReplyTo, hubspotGRPCCli)
		log.Printf("[otpsender] resend from=%s hubspotSync=%v", cfg.ResendFromEmail, hubspotGRPCCli != nil)
	}

	if _, ok := otpSender.(noopOTPSender); ok {
		log.Printf("[otpsender] NoOp — no real provider configured")
	}

	// JWT (signer + verifier construidos sobre la lib compartida).
	signer, err := jwtmw.NewSigner(jwtmw.SignerConfig{
		Secret:     []byte(cfg.JWTSecret),
		Issuer:     cfg.JWTIssuer,
		AccessTTL:  cfg.JWTAccessTTL,
		RefreshTTL: cfg.JWTRefreshTTL,
	})
	if err != nil {
		log.Fatalf("jwt signer: %v", err)
	}
	verifier := jwtmw.NewVerifier([]byte(cfg.JWTSecret), cfg.JWTIssuer)

	tokenIssuer := jwtadapter.NewIssuer(signer)
	tokenVerifier := jwtadapter.NewVerifier(verifier)

	// ---------- 3. CORE — CQRS: commands y queries son piezas separadas ----------
	userCmds := command.NewUserHandler(userRepo, permRepo, cache, hasher, hubspotSync)
	authCmds := command.NewAuthHandler(userRepo, schoolRepo, permRepo, cache, hasher, tokenIssuer, tokenVerifier, refreshRepo, otpRepo, otpHasher, otpSender, studentClassifier)
	permCmds := command.NewPermissionHandler(userRepo, permRepo)
	permGroupCmds := command.NewPermissionGroupHandler(permRepo)
	assignmentCmds := command.NewAssignmentHandler(userRepo, assignmentRepo)
	hubspotSyncCmds := command.NewHubspotSyncHandler(userRepo, schoolRepo)
	userQrys := query.NewUserHandler(userRepo, cache, permRepo)
	permQrys := query.NewPermissionHandler(userRepo, permRepo)
	permGroupQrys := query.NewPermissionGroupHandler(permRepo)
	assignmentQrys := query.NewAssignmentHandler(assignmentRepo)

	// Estos handlers todavía no se exponen por gRPC; quedan listos para
	// que un AdminService los consuma cuando se modelen sus RPCs.
	_ = assignmentCmds
	_ = assignmentQrys
	_ = hubspotSyncCmds

	// ---------- 4. ADAPTERS INBOUND gRPC ----------
	userHandler := grpchandler.NewUserHandler(userCmds, userQrys, permCmds, permQrys)
	authHandler := grpchandler.NewAuthHandler(authCmds)
	permGroupHandler := grpchandler.NewPermissionGroupHandler(permGroupCmds, permGroupQrys)

	// ---------- 5. SERVIDOR gRPC ----------
	lis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	// JWT validation se aplica a todos los métodos de UserService;
	// AuthService está en la skip-list (no se puede pedir token para
	// pedir un token).
	jwtSkip := func(fullMethod string) bool {
		return strings.HasPrefix(fullMethod, "/users.v1.AuthService/") ||
			strings.HasPrefix(fullMethod, "/grpc.health.") ||
			strings.HasPrefix(fullMethod, "/grpc.reflection.")
	}

	// Audit interceptor: registra cada mutación en audit_log. El bridge
	// adapta la interfaz de auditmw a ports.AuditSink (mssql).
	auditBridge := auditadapter.NewBridge(auditSink)

	// PERMISSIONS interceptor: cada request consulta la BD de permisos
	// EN VIVO (no los claims del JWT). users-service usa un resolver
	// LOCAL para evitar el self-loop gRPC. Otros microservicios usan
	// permsclient.Client (gRPC + cache Redis 30s).
	localPerms := grpchandler.NewLocalPermissionResolver(permQrys)

	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpchandler.RecoveryInterceptor,
			grpchandler.CorrelationIDInterceptor,
			grpchandler.LoggingInterceptor,
			jwtmw.UnaryServerInterceptor(verifier, jwtSkip),
			permmw.UnaryServerInterceptor(localPerms, grpchandler.PermissionMap),
			auditmw.UnaryServerInterceptor(auditBridge, redactSensitiveFields),
		),
	)
	pb.RegisterUserServiceServer(s, userHandler)
	pb.RegisterAuthServiceServer(s, authHandler)
	pb.RegisterSchoolServiceServer(s, grpchandler.NewSchoolHandler(schoolRepo, assignmentRepo))
	pb.RegisterVisitaServiceServer(s, grpchandler.NewVisitaHandler(visitaRepo))
	pb.RegisterPermissionGroupServiceServer(s, permGroupHandler)

	// Health check (usado por readiness/liveness de Kubernetes).
	hs := health.NewServer()
	healthpb.RegisterHealthServer(s, hs)
	hs.SetServingStatus("users.v1.UserService", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("users.v1.AuthService", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("users.v1.SchoolService", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("users.v1.VisitaService", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("users.v1.PermissionGroupService", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	reflection.Register(s) // para debuggear con grpcurl

	// ---------- 6. RUN + GRACEFUL SHUTDOWN ----------
	errCh := make(chan error, 1)
	go func() {
		log.Printf("users_service gRPC listening on %s", cfg.GRPCPort)
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

// redactSensitiveFields produce una copia del request con campos
// sensibles vaciados, para que el audit log no guarde passwords ni
// tokens en texto plano. Usa proto.Reflect para no acoplarse a tipos
// concretos: cualquier mensaje que tenga un field "password",
// "refresh_token" o "access_token" se ofusca automáticamente.
var sensitiveFieldNames = map[string]struct{}{
	"password":      {},
	"refresh_token": {},
	"access_token":  {},
	"otp":           {},
	"token":         {},
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

// noopOTPSender se usa cuando HUBSPOT_SERVICE_ADDR está vacío (dev local).
// Loguea el OTP en plain — solo para development; en prod siempre debe
// estar wireado a otpsenderadapter.NewGrpc.
type noopOTPSender struct{}

var _ ports.OTPSender = noopOTPSender{}

func (noopOTPSender) Send(_ context.Context, email, otp string) error {
	log.Printf("[otpsender:noop] would send OTP %q to %q", otp, email)
	return nil
}

func (n noopOTPSender) SendWithContact(ctx context.Context, in ports.OTPSendInput) error {
	return n.Send(ctx, in.Email, in.PlainOTP)
}

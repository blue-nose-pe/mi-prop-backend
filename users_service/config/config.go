package config

import (
	"log"
	"os"
	"strconv"
	"time"
)

// Config agrupa toda la configuración del microservicio.
// Se llena desde variables de entorno (las inyecta el ConfigMap/Secret de K8s).
type Config struct {
	GRPCPort string

	// Azure SQL Database
	SQLServer          string // ej "miproposito-sql.database.windows.net"
	SQLPort            int    // 1433
	SQLDatabase        string // ej "db_users"
	SQLUser            string
	SQLPassword        string
	SQLEncrypt         bool
	SQLTrustServerCert bool

	// Redis
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisTLS      bool
	CacheTTL      time.Duration

	BcryptCost   int
	ShutdownWait time.Duration

	// JWT
	JWTSecret     string
	JWTIssuer     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration

	// HubSpot service (gRPC) — para enviar OTP por email vía hubspot_service.
	// Si está vacío, el OTP sender es NoOp (solo dev local).
	HubspotServiceAddr string

	// OTPSender:
	//   "smtp"    → SMTP AUTH PLAIN sobre STARTTLS (Gmail). ENVÍO DEFINITIVO.
	//   "hubspot" → path antiguo: hubspot_service.SendOTP dispara Workflow.
	//   vacio     → default "hubspot".
	// (El proveedor de terceros "resend" se retiró — código muerto.)
	OTPSender string

	// SMTP (solo cuando OTP_SENDER=smtp). Gmail: smtp.gmail.com:587 +
	// App Password de https://myaccount.google.com/apppasswords.
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string // "Mi Proposito UCSP <tu-email@gmail.com>"

	// FrontBaseURL: base publica del front (ej https://miproposito.ucsp.edu.pe
	// o http://15.156.100.140). La usa el correo del MASIVO para armar el magic
	// link <front>/test/simulacro/ingresar?e=&c=. "" = el correo solo lleva el
	// codigo (sin boton de acceso directo).
	FrontBaseURL string
}

func Load() *Config {
	c := &Config{
		GRPCPort: getEnv("GRPC_PORT", ":50051"),

		SQLServer:          getEnv("SQL_SERVER", "127.0.0.1"),
		SQLPort:            getEnvInt("SQL_PORT", 1433),
		SQLDatabase:        getEnv("SQL_DATABASE", "db_users"),
		SQLUser:            getEnv("SQL_USER", "sa"),
		SQLPassword:        getEnv("SQL_PASSWORD", ""),
		SQLEncrypt:         getEnvBool("SQL_ENCRYPT", true),
		SQLTrustServerCert: getEnvBool("SQL_TRUST_SERVER_CERT", false),

		RedisAddr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),
		RedisTLS:      getEnvBool("REDIS_TLS", false),
		CacheTTL:      getEnvDuration("CACHE_TTL", 15*time.Minute),

		BcryptCost:   getEnvInt("BCRYPT_COST", 10),
		ShutdownWait: getEnvDuration("SHUTDOWN_WAIT", 10*time.Second),

		JWTSecret:     getEnv("JWT_SECRET", ""),
		JWTIssuer:     getEnv("JWT_ISSUER", "miproposito.users"),
		JWTAccessTTL:  getEnvDuration("JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL: getEnvDuration("JWT_REFRESH_TTL", 7*24*time.Hour),

		HubspotServiceAddr: getEnv("HUBSPOT_SERVICE_ADDR", ""),

		OTPSender: getEnv("OTP_SENDER", "hubspot"),

		SMTPHost:     getEnv("SMTP_HOST", ""),
		SMTPPort:     getEnvInt("SMTP_PORT", 587),
		SMTPUsername: getEnv("SMTP_USERNAME", ""),
		SMTPPassword: getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:     getEnv("SMTP_FROM", ""),
		FrontBaseURL: getEnv("FRONT_BASE_URL", ""),
	}
	log.Printf("[config] grpc=%s sql=%s/%s redis=%s hubspot=%s otpsender=%s", c.GRPCPort, c.SQLServer, c.SQLDatabase, c.RedisAddr, c.HubspotServiceAddr, c.OTPSender)
	return c
}

func getEnv(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}

func getEnvInt(k string, def int) int {
	if v, ok := os.LookupEnv(k); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvBool(k string, def bool) bool {
	if v, ok := os.LookupEnv(k); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func getEnvDuration(k string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(k); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

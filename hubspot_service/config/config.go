package config

import (
	"log"
	"os"
	"strconv"
	"time"
)

type Config struct {
	GRPCPort    string
	WebhookPort string

	// HubSpot.
	HubspotToken         string
	HubspotEnvironment   string // prod | dev
	CustomKeyTypeID      string
	CustomAsesorTypeID   string
	CustomColegioTypeID  string
	OTPWebhookTriggerID  string
	OTPWebhookToken      string

	// Redis (asynq).
	RedisAddr     string
	RedisPassword string
	RedisTLS      bool

	// Auth.
	JWTSecret string
	JWTIssuer string

	// Worker tuning.
	WorkerConcurrency int

	ShutdownWait time.Duration
}

func Load() *Config {
	c := &Config{
		GRPCPort:            getEnv("GRPC_PORT", ":50054"),
		WebhookPort:         getEnv("WEBHOOK_HTTP_PORT", ":8080"),
		HubspotToken:        getEnv("HUBSPOT_API_TOKEN", ""),
		HubspotEnvironment:  getEnv("HUBSPOT_ENVIRONMENT", "prod"),
		CustomKeyTypeID:     getEnv("HUBSPOT_CO_KEY_ID", "2-32450705"),
		CustomAsesorTypeID:  getEnv("HUBSPOT_CO_ASESOR_ID", "2-32448565"),
		CustomColegioTypeID: getEnv("HUBSPOT_CO_COLEGIO_ID", "2-32450269"),
		OTPWebhookTriggerID: getEnv("HUBSPOT_OTP_WEBHOOK_TRIGGER_ID", "9013951"),
		OTPWebhookToken:     getEnv("HUBSPOT_OTP_WEBHOOK_TOKEN", ""),

		RedisAddr:     getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisTLS:      getEnvBool("REDIS_TLS", false),

		JWTSecret: getEnv("JWT_SECRET", ""),
		JWTIssuer: getEnv("JWT_ISSUER", "miproposito.users"),

		WorkerConcurrency: getEnvInt("WORKER_CONCURRENCY", 10),

		ShutdownWait: getEnvDuration("SHUTDOWN_WAIT", 10*time.Second),
	}
	log.Printf("[config] grpc=%s webhook=%s hubspot_env=%s redis=%s",
		c.GRPCPort, c.WebhookPort, c.HubspotEnvironment, c.RedisAddr)
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

package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	GRPCPort    string
	WebhookPort string

	// HubSpot — pool de tokens para el rate limiter distribuido.
	//
	// HubspotTokens es la fuente canónica (lista de API keys). Si está
	// vacía, se cae a HubspotToken (singular, backwards compat) tratado
	// como pool de 1 token.
	//
	// Cuando hubspot-service tiene >1 réplica, el rate limit se coordina
	// entre todas vía Redis (paquete requestsengine). Sin coordinación,
	// las N réplicas saturan la misma key y reciben 429.
	HubspotTokens       []string // CSV en HUBSPOT_API_TOKENS
	HubspotToken        string   // legacy — pool de 1 si HubspotTokens está vacío
	HubspotRPS          int      // rps por token. HubSpot = 10. Default 10.
	HubspotWorkers      int      // workers internos del engine. Default 10.
	HubspotQueueSize    int      // buffer del canal del engine. Default 100.
	HubspotEnvironment  string   // prod | dev
	CustomKeyTypeID     string
	CustomAsesorTypeID  string
	CustomColegioTypeID string
	OTPWebhookTriggerID string
	OTPWebhookToken     string

	// Redis (asynq + requestsengine).
	RedisAddr        string
	RedisPassword    string
	RedisTLS         bool
	RedisDB          int // asynq
	RedisDBRateLimit int // requestsengine — DB separada para no chocar con asynq

	// Auth.
	JWTSecret string
	JWTIssuer string

	// Worker tuning.
	WorkerConcurrency int

	ShutdownWait time.Duration
}

// EffectiveTokens devuelve los tokens activos. Prefiere HubspotTokens
// (lista) sobre HubspotToken (legacy). Devuelve nil si no hay ninguno.
func (c *Config) EffectiveTokens() []string {
	if len(c.HubspotTokens) > 0 {
		return c.HubspotTokens
	}
	if c.HubspotToken != "" {
		return []string{c.HubspotToken}
	}
	return nil
}

func Load() *Config {
	c := &Config{
		GRPCPort:            getEnv("GRPC_PORT", ":50054"),
		WebhookPort:         getEnv("WEBHOOK_HTTP_PORT", ":8080"),
		HubspotTokens:       splitCSV(getEnv("HUBSPOT_API_TOKENS", "")),
		HubspotToken:        getEnv("HUBSPOT_API_TOKEN", ""),
		HubspotRPS:          getEnvInt("HUBSPOT_RPS", 10),
		HubspotWorkers:      getEnvInt("HUBSPOT_ENGINE_WORKERS", 10),
		HubspotQueueSize:    getEnvInt("HUBSPOT_ENGINE_QUEUE_SIZE", 100),
		HubspotEnvironment:  getEnv("HUBSPOT_ENVIRONMENT", "prod"),
		CustomKeyTypeID:     getEnv("HUBSPOT_CO_KEY_ID", "2-32450705"),
		CustomAsesorTypeID:  getEnv("HUBSPOT_CO_ASESOR_ID", "2-32448565"),
		CustomColegioTypeID: getEnv("HUBSPOT_CO_COLEGIO_ID", "2-32450269"),
		OTPWebhookTriggerID: getEnv("HUBSPOT_OTP_WEBHOOK_TRIGGER_ID", "9013951"),
		OTPWebhookToken:     getEnv("HUBSPOT_OTP_WEBHOOK_TOKEN", ""),

		RedisAddr:        getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:    getEnv("REDIS_PASSWORD", ""),
		RedisTLS:         getEnvBool("REDIS_TLS", false),
		RedisDB:          getEnvInt("REDIS_DB", 0),
		RedisDBRateLimit: getEnvInt("REDIS_DB_RATELIMIT", 1),

		JWTSecret: getEnv("JWT_SECRET", ""),
		JWTIssuer: getEnv("JWT_ISSUER", "miproposito.users"),

		WorkerConcurrency: getEnvInt("WORKER_CONCURRENCY", 10),

		ShutdownWait: getEnvDuration("SHUTDOWN_WAIT", 10*time.Second),
	}
	log.Printf("[config] grpc=%s webhook=%s hubspot_env=%s tokens=%d rps=%d redis=%s (db=%d, rl_db=%d)",
		c.GRPCPort, c.WebhookPort, c.HubspotEnvironment,
		len(c.EffectiveTokens()), c.HubspotRPS,
		c.RedisAddr, c.RedisDB, c.RedisDBRateLimit)
	return c
}

func splitCSV(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
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

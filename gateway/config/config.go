package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPPort string

	JWTSecret string
	JWTIssuer string

	// Direcciones gRPC de los upstreams.
	UsersServiceAddr        string
	ExamsServiceAddr        string
	KeysServiceAddr         string
	HubspotServiceAddr      string
	SatisfactionServiceAddr string
	AnalyticsServiceAddr    string

	// CORS.
	CORSAllowedOrigins []string

	// Rate limit.
	RateLimitEnabled        bool
	RateLimitPerIPPerMinute int
	RedisAddr               string
	RedisPassword           string

	ShutdownWait time.Duration
}

func Load() *Config {
	c := &Config{
		HTTPPort:                getEnv("HTTP_PORT", ":8080"),
		JWTSecret:               getEnv("JWT_SECRET", ""),
		JWTIssuer:               getEnv("JWT_ISSUER", "miproposito.users"),
		UsersServiceAddr:        getEnv("USERS_SERVICE_ADDR", "users-service:50051"),
		ExamsServiceAddr:        getEnv("EXAMS_SERVICE_ADDR", "exams-service:50052"),
		KeysServiceAddr:         getEnv("KEYS_SERVICE_ADDR", "keys-service:50053"),
		HubspotServiceAddr:      getEnv("HUBSPOT_SERVICE_ADDR", "hubspot-service:50054"),
		SatisfactionServiceAddr: getEnv("SATISFACTION_SERVICE_ADDR", "satisfaction-service:50055"),
		AnalyticsServiceAddr:    getEnv("ANALYTICS_SERVICE_ADDR", "analytics-service:50056"),
		CORSAllowedOrigins:      splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "https://miproposito.ucsp.edu.pe")),
		RateLimitEnabled:        getEnvBool("RATELIMIT_ENABLED", true),
		RateLimitPerIPPerMinute: getEnvInt("RATELIMIT_PER_IP_PER_MIN", 600),
		RedisAddr:               getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:           getEnv("REDIS_PASSWORD", ""),
		ShutdownWait:            getEnvDuration("SHUTDOWN_WAIT", 10*time.Second),
	}
	log.Printf("[config] http=%s upstreams users:%s exams:%s keys:%s hs:%s sat:%s an:%s",
		c.HTTPPort, c.UsersServiceAddr, c.ExamsServiceAddr, c.KeysServiceAddr,
		c.HubspotServiceAddr, c.SatisfactionServiceAddr, c.AnalyticsServiceAddr)
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

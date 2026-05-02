package config

import (
	"log"
	"os"
	"strconv"
	"time"
)

type Config struct {
	GRPCPort string

	JWTSecret string
	JWTIssuer string

	// Upstreams.
	UsersServiceAddr        string
	ExamsServiceAddr        string
	KeysServiceAddr         string
	SatisfactionServiceAddr string

	// Cache (Redis).
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	CacheEnabled  bool

	ShutdownWait time.Duration
}

func Load() *Config {
	c := &Config{
		GRPCPort:                getEnv("GRPC_PORT", ":50056"),
		JWTSecret:               getEnv("JWT_SECRET", ""),
		JWTIssuer:               getEnv("JWT_ISSUER", "miproposito.users"),
		UsersServiceAddr:        getEnv("USERS_SERVICE_ADDR", "users-service:50051"),
		ExamsServiceAddr:        getEnv("EXAMS_SERVICE_ADDR", "exams-service:50052"),
		KeysServiceAddr:         getEnv("KEYS_SERVICE_ADDR", "keys-service:50053"),
		SatisfactionServiceAddr: getEnv("SATISFACTION_SERVICE_ADDR", "satisfaction-service:50055"),
		RedisAddr:               getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:           getEnv("REDIS_PASSWORD", ""),
		RedisDB:                 getEnvInt("REDIS_DB", 1),
		CacheEnabled:            getEnvBool("CACHE_ENABLED", true),
		ShutdownWait:            getEnvDuration("SHUTDOWN_WAIT", 10*time.Second),
	}
	log.Printf("[config] grpc=%s upstreams=[users:%s exams:%s keys:%s] cache=%v",
		c.GRPCPort, c.UsersServiceAddr, c.ExamsServiceAddr, c.KeysServiceAddr, c.CacheEnabled)
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

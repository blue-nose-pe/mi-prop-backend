// Package config carga el config del servicio desde env vars.
package config

import (
	"log"
	"os"
	"strconv"
	"time"
)

type Config struct {
	GRPCPort string

	SQLServer          string
	SQLPort            int
	SQLDatabase        string
	SQLUser            string
	SQLPassword        string
	SQLEncrypt         bool
	SQLTrustServerCert bool

	JWTSecret string
	JWTIssuer string

	KeysServiceAddr string // gRPC keys_service (ej: "keys-service:50053")

	ShutdownWait time.Duration
}

func Load() *Config {
	c := &Config{
		GRPCPort: getEnv("GRPC_PORT", ":50052"),

		SQLServer:          getEnv("SQL_SERVER", "127.0.0.1"),
		SQLPort:            getEnvInt("SQL_PORT", 1433),
		SQLDatabase:        getEnv("SQL_DATABASE", "db_exams"),
		SQLUser:            getEnv("SQL_USER", "sa"),
		SQLPassword:        getEnv("SQL_PASSWORD", ""),
		SQLEncrypt:         getEnvBool("SQL_ENCRYPT", true),
		SQLTrustServerCert: getEnvBool("SQL_TRUST_SERVER_CERT", false),

		JWTSecret: getEnv("JWT_SECRET", ""),
		JWTIssuer: getEnv("JWT_ISSUER", "miproposito.users"),

		KeysServiceAddr: getEnv("KEYS_SERVICE_ADDR", ""),
		ShutdownWait:    getEnvDuration("SHUTDOWN_WAIT", 10*time.Second),
	}
	log.Printf("[config] grpc=%s sql=%s/%s keys=%s", c.GRPCPort, c.SQLServer, c.SQLDatabase, c.KeysServiceAddr)
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

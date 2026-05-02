// Binario que aplica migraciones T-SQL a db_satisfaction (k8s Job).
package main

import (
	"context"
	"log"
	"time"

	"satisfaction_service/config"
	"satisfaction_service/db"
	"satisfaction_service/internal/shared/migrator"
	"satisfaction_service/internal/shared/mssql"
)

type stdLogger struct{}

func (stdLogger) Info(msg string, kv ...any)  { log.Printf("INFO  "+msg+" %v", kv) }
func (stdLogger) Error(msg string, kv ...any) { log.Printf("ERROR "+msg+" %v", kv) }

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	conn, err := mssql.Open(ctx, mssql.Config{
		Server:          cfg.SQLServer,
		Port:            cfg.SQLPort,
		Database:        cfg.SQLDatabase,
		User:            cfg.SQLUser,
		Password:        cfg.SQLPassword,
		Encrypt:         cfg.SQLEncrypt,
		TrustServerCert: cfg.SQLTrustServerCert,
		AppName:         "satisfaction-service-migrate",
	})
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	runner := migrator.New(conn, db.Migrations, db.Subdir, stdLogger{})
	if err := runner.Run(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Printf("migrations applied successfully")
}

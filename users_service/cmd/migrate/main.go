// Binario que aplica las migraciones T-SQL a la BD del servicio.
// Pensado para correr como k8s Job (helm pre-install/pre-upgrade hook).
//
// Idempotente:
//   - Las migraciones ya aplicadas se saltan (tabla __schema_migrations).
//   - Si una migración ya aplicada fue editada (checksum distinto),
//     aborta en vez de aplicar algo distinto en silencio.
//
// Las migraciones se embeben en el paquete users_service/db → el binario
// es self-contained.
//
// Variables de entorno (las inyecta el Job de Helm):
//   SQL_SERVER, SQL_PORT, SQL_DATABASE, SQL_USER, SQL_PASSWORD,
//   SQL_ENCRYPT, SQL_TRUST_SERVER_CERT
package main

import (
	"context"
	"log"
	"time"

	"users_service/config"
	"users_service/db"
	"users_service/internal/shared/migrator"
	"users_service/internal/shared/mssql"
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
		AppName:         "users-service-migrate",
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

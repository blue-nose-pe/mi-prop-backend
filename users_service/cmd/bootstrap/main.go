// Binario `bootstrap` — corre como k8s Job (helm hook post-install) y
// crea el PRIMER superadmin del sistema si todavía no existe ninguno.
//
// Idempotente: si ya hay algún user con `is_superadmin = 1`, sale sin
// hacer nada (exit 0). Eso permite que el chart se reinstale o se
// upgradee sin duplicar superadmins.
//
// La password viene del Secret k8s `<release>-superadmin-credentials`
// (ver Helm chart). El admin la lee una sola vez con kubectl después
// del install — el primer login fuerza al user a cambiarla por una
// definitiva (must_change_password=1).
//
// Variables de entorno:
//   SUPERADMIN_EMAIL    — email del superadmin a crear (obligatorio)
//   SUPERADMIN_PASSWORD — password en CLARO (obligatorio; el job la
//                         lee desde el Secret de Helm)
//   SQL_*               — los mismos que cmd/migrate
package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"users_service/config"
	bcryptadapter "users_service/internal/adapters/outbound/bcrypt"
	mssqladapter "users_service/internal/adapters/outbound/mssql"
	"users_service/internal/core/domain"
	"users_service/internal/shared/mssql"
)

func main() {
	cfg := config.Load()

	email := strings.TrimSpace(strings.ToLower(os.Getenv("SUPERADMIN_EMAIL")))
	password := os.Getenv("SUPERADMIN_PASSWORD")
	if email == "" || password == "" {
		log.Fatalf("bootstrap: SUPERADMIN_EMAIL and SUPERADMIN_PASSWORD are required")
	}
	if err := domain.Email(email).Validate(); err != nil {
		log.Fatalf("bootstrap: invalid email %q: %v", email, err)
	}
	if err := domain.ValidatePasswordStrength(password); err != nil {
		log.Fatalf("bootstrap: weak password (Helm Secret debería garantizar 24 chars): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := mssql.Open(ctx, mssql.Config{
		Server:          cfg.SQLServer,
		Port:            cfg.SQLPort,
		Database:        cfg.SQLDatabase,
		User:            cfg.SQLUser,
		Password:        cfg.SQLPassword,
		Encrypt:         cfg.SQLEncrypt,
		TrustServerCert: cfg.SQLTrustServerCert,
		AppName:         "users-service-bootstrap",
	})
	if err != nil {
		log.Fatalf("bootstrap: connect: %v", err)
	}
	defer db.Close()

	users := mssqladapter.NewUserRepo(db)

	exists, err := users.ExistsAnySuperadmin(ctx)
	if err != nil {
		log.Fatalf("bootstrap: check superadmin: %v", err)
	}
	if exists {
		log.Printf("bootstrap: superadmin already exists — nothing to do")
		return
	}

	hasher := bcryptadapter.New(cfg.BcryptCost)
	hash, err := hasher.Hash(password)
	if err != nil {
		log.Fatalf("bootstrap: hash: %v", err)
	}

	id, err := users.SaveSuperadmin(ctx, email, hash)
	if err != nil {
		log.Fatalf("bootstrap: save superadmin: %v", err)
	}
	log.Printf("bootstrap: superadmin created id=%s email=%s", id, email)
	log.Printf("bootstrap: must_change_password=true — el user será forzado a cambiar la password en su primer login")
}

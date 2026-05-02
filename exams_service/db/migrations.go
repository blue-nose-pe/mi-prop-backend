// Package db expone las migrations T-SQL del servicio como embed.FS,
// para que cmd/migrate las aplique vía internal/shared/migrator.
package db

import "embed"

//go:embed all:migrations
var Migrations embed.FS

const Subdir = "migrations"
